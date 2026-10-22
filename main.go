// hostdeck is a small console for your own computer: an overview of status
// cards and, behind a sidebar, the web UIs of the services running on it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Ike-li/hostdeck/internal/console"
	"github.com/Ike-li/hostdeck/internal/onetime"
	"github.com/Ike-li/hostdeck/internal/plugin"
	"github.com/Ike-li/hostdeck/internal/ports"
	"github.com/Ike-li/hostdeck/internal/proxy"
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("hostdeck", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:4678", "address the console listens on; the host must be 127.0.0.1")
	dirFlag := flags.String("dir", "", "folder holding plugins/ (default: $HOSTDECK_DIR, else the per-user config folder)")
	if err := flags.Parse(args); err != nil {
		return err
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
	plugins, broken, err := plugin.Load(pluginDir)
	if err != nil {
		return err
	}
	for _, b := range broken {
		log.Printf("%s: not loaded: %v", b.ID, b.Err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	consolePort := ln.Addr().(*net.TCPAddr).Port
	tokens := onetime.New(time.Now)
	served, err := serveProxies(plugins, filepath.Join(dir, "ports.json"), consolePort, tokens)
	if err != nil {
		return err
	}
	log.Printf("hostdeck: http://%s/ (%d plugins in %s)", ln.Addr(), len(plugins), pluginDir)
	return http.Serve(ln, console.Handler(console.Config{
		Origin:    fmt.Sprintf("http://127.0.0.1:%d", consolePort),
		Plugins:   plugins,
		Broken:    broken,
		PluginDir: pluginDir,
		Served:    served,
		Tokens:    tokens,
	}))
}

// serveProxies re-serves each plugin's proxied page on a port of its own, from
// the port after the console's up, and says where the browser finds each one.
// Each proxy clears its service's browser data with a token from tokens.
func serveProxies(plugins []plugin.Plugin, portsFile string, consolePort int, tokens *onetime.Tokens) (map[string]console.Served, error) {
	var ids []string
	for _, p := range plugins {
		if p.Page != nil && p.Page.Proxy != "" {
			ids = append(ids, p.ID)
		}
	}
	opened, err := ports.Listen(portsFile, ids, consolePort+1)
	if err != nil {
		return nil, err
	}
	consoleOrigin := fmt.Sprintf("http://127.0.0.1:%d", consolePort)
	served := make(map[string]console.Served, len(ids))
	for _, p := range plugins {
		o, ok := opened[p.ID]
		if !ok {
			continue
		}
		if o.Err != nil {
			log.Printf("%s: %v", p.ID, o.Err)
			served[p.ID] = console.Served{Err: o.Err}
			continue
		}
		target, err := url.Parse(p.Page.Proxy)
		if err != nil {
			return nil, err
		}
		served[p.ID] = console.Served{URL: fmt.Sprintf("http://127.0.0.1:%d%s", o.Port, target.RequestURI())}
		log.Printf("%s: http://127.0.0.1:%d/ -> %s", p.ID, o.Port, p.Page.Proxy)
		go func() {
			redeem := func(token string) bool { return tokens.Redeem(p.ID, token) }
			log.Printf("%s: %v", p.ID, http.Serve(o.Listener, proxy.New(target, consoleOrigin, redeem)))
		}()
	}
	return served, nil
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
