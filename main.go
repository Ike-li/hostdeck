// hostdeck is a small console for your own computer: an overview of status
// cards and, behind a sidebar, the web UIs of the services running on it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Ike-li/hostdeck/internal/console"
	"github.com/Ike-li/hostdeck/internal/plugin"
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
	plugins, err := plugin.Load(pluginDir)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	log.Printf("hostdeck: http://%s/ (%d plugins in %s)", ln.Addr(), len(plugins), pluginDir)
	return http.Serve(ln, console.Handler(plugins, pluginDir))
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
