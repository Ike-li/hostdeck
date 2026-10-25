// hostdeck is a small console for your own computer: an overview of status
// cards and, behind a sidebar, the web UIs of the services running on it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ike-li/hostdeck/examples"
	"github.com/Ike-li/hostdeck/internal/cli"
	"github.com/Ike-li/hostdeck/internal/console"
	"github.com/Ike-li/hostdeck/internal/diskhistory"
	"github.com/Ike-li/hostdeck/internal/layout"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
	"github.com/Ike-li/hostdeck/internal/proxy"
)

func main() {
	log.SetFlags(0)
	if args := os.Args[1:]; len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		os.Exit(cli.Run(args, os.Stdout))
	}
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("hostdeck", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: hostdeck [--dir DATA_DIRECTORY] [--addr 127.0.0.1:4678]")
		flags.PrintDefaults()
		fmt.Fprintln(flags.Output(), "Connection commands for agents: hostdeck help")
	}
	addr := flags.String("addr", "127.0.0.1:4678", "address the console listens on; the host must be 127.0.0.1")
	dirFlag := flags.String("dir", "", "folder holding plugins/ (default: $HOSTDECK_DIR, else the per-user config folder)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected server arguments; connection commands start with page or plugin")
	}
	if err := checkAddr(*addr); err != nil {
		return err
	}
	dir, err := configDir(*dirFlag)
	if err != nil {
		return err
	}
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	consolePort := ln.Addr().(*net.TCPAddr).Port
	tokens := onetime.New(time.Now)
	f := &forwarders{portsFile: filepath.Join(dir, "ports.json"), consolePort: consolePort, tokens: tokens}
	// The console calls reload one change at a time.
	reload := func() (console.Loaded, error) {
		plugins, broken, err := plugin.Load(pluginDir)
		if err != nil {
			return console.Loaded{}, err
		}
		for _, b := range broken {
			log.Printf("%s: not loaded: %v", b.ID, b.Err)
		}
		served, err := f.sync(plugins)
		if err != nil {
			return console.Loaded{}, err
		}
		diskhistory.Sync(pluginDir, plugins)
		log.Printf("hostdeck: %d plugins in %s", len(plugins), pluginDir)
		return console.Loaded{Plugins: plugins, Broken: broken, Served: served}, nil
	}
	loaded, err := reload()
	if err != nil {
		return err
	}
	arranged, err := layout.Open(filepath.Join(dir, "layout.json"))
	if err != nil {
		return err
	}
	log.Printf("hostdeck: http://%s/", ln.Addr())
	return http.Serve(ln, console.Handler(console.Config{
		Origin:    fmt.Sprintf("http://127.0.0.1:%d", consolePort),
		Plugins:   loaded.Plugins,
		Broken:    loaded.Broken,
		PluginDir: pluginDir,
		BuiltIn:   examples.Files,
		Served:    loaded.Served,
		Tokens:    tokens,
		Layout:    arranged,
		Reload:    reload,
	}))
}

// forwarders re-serve each plugin's proxied page on a port of its own, from
// the port after the console's up. Each proxy clears its service's browser
// data with a token from tokens.
type forwarders struct {
	portsFile   string
	consolePort int
	tokens      *onetime.Tokens
	running     map[string]*forwarder // by plugin ID
}

type forwarder struct {
	target string // the manifest's page.proxy
	url    string // where the browser finds it
	server *http.Server
}

// sync serves the proxied pages of plugins, and only those, and says where the
// browser finds each one. A service keeps its port when it stops and starts
// again; one whose port could not be opened is tried again on the next sync.
func (f *forwarders) sync(plugins []plugin.Plugin) (map[string]console.Served, error) {
	if f.running == nil {
		f.running = map[string]*forwarder{}
	}
	want := map[string]string{}
	var ids []string
	for _, p := range plugins {
		if p.Page == nil || p.Page.Proxy == "" {
			continue
		}
		want[p.ID] = p.Page.Proxy
		if r, ok := f.running[p.ID]; !ok || r.target != p.Page.Proxy {
			ids = append(ids, p.ID)
		}
	}
	// Stopped first, so that a service pointed elsewhere gets its port back.
	for id, r := range f.running {
		if want[id] != r.target {
			r.server.Close()
			delete(f.running, id)
			log.Printf("%s: %s stopped", id, r.url)
		}
	}
	opened, err := ports.Listen(f.portsFile, ids, f.consolePort+1)
	if err != nil {
		return nil, err
	}
	consoleOrigin := fmt.Sprintf("http://127.0.0.1:%d", f.consolePort)
	served := make(map[string]console.Served, len(want))
	for _, id := range ids {
		o := opened[id]
		if o.Err != nil {
			log.Printf("%s: %v", id, o.Err)
			served[id] = console.Served{Err: o.Err}
			continue
		}
		target, err := url.Parse(want[id])
		if err != nil {
			o.Listener.Close()
			served[id] = console.Served{Err: err}
			continue
		}
		redeem := func(token string) bool { return f.tokens.Redeem(id, token) }
		r := &forwarder{
			target: want[id],
			url:    fmt.Sprintf("http://127.0.0.1:%d%s", o.Port, target.RequestURI()),
			server: &http.Server{Handler: proxy.New(target, consoleOrigin, redeem)},
		}
		f.running[id] = r
		log.Printf("%s: http://127.0.0.1:%d/ -> %s", id, o.Port, r.target)
		go func() {
			if err := r.server.Serve(o.Listener); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("%s: %v", id, err)
			}
		}()
	}
	for id, r := range f.running {
		served[id] = console.Served{URL: r.url}
	}
	return served, nil
}

func (f *forwarders) close() {
	for _, r := range f.running {
		r.server.Close()
	}
}

// checkAddr allows 127.0.0.1 only. Anything else either reaches beyond this
// machine, which the security model rules out, or gives the console an origin
// other than http://127.0.0.1, which the embedded services are not told to trust.
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("--addr %s: hostdeck listens on 127.0.0.1 only, e.g. 127.0.0.1:4678", addr)
	}
	return nil
}

// configDir is the folder holding plugins/: --dir, else $HOSTDECK_DIR, else a
// per-user folder that defaultDir picks for each platform.
func configDir(flagDir string) (string, error) {
	if flagDir != "" {
		return flagDir, nil
	}
	if env := os.Getenv("HOSTDECK_DIR"); env != "" {
		return env, nil
	}
	return defaultDir()
}
