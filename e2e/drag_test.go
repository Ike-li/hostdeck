package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestDraggingConsoleLinksDoesNotAddPluginsAndExternalAddressesStillWork(t *testing.T) {
	dir := t.TempDir()
	svc := framingRefusingService(t)
	writePlugin(t, dir, "svc", fmt.Sprintf(proxyPlugin, svc))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url+"#/svc"), chromedp.WaitVisible(chromedp.CSS(`#service-open`)))
	events := chromedp.Events(ctx, network.RequestWillBeSent)
	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		t.Fatal(err)
	}
	brandDraggable, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('.brand').draggable`))
	if err != nil {
		t.Fatal(err)
	}
	if brandDraggable {
		t.Error("the brand link should not start a native link drag")
	}
	for _, selector := range []string{".brand", "#nav-overview", "#nav-catalog", "#service-open"} {
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => {
   const source = document.querySelector(%q), target = document.querySelector('#nav');
   const dt = new DataTransfer(); dt.setData('text/uri-list',source.href);
   source.dispatchEvent(new DragEvent('dragstart',{bubbles:true,cancelable:true,dataTransfer:dt}));
   for (const type of ['dragover','drop']) target.dispatchEvent(new DragEvent(type,{bubbles:true,cancelable:true,dataTransfer:dt}));
   source.dispatchEvent(new DragEvent('dragend',{bubbles:true,dataTransfer:dt}));
  })()`, selector))); err != nil {
			t.Fatal(err)
		}
	}
	// An address dragged in from outside has no in-document dragstart. The
	// console's own origin must still be ignored, including its plugin routes.
	for _, address := range []string{url + "/#/", url + "/#/127-0-0-1-4678-2"} {
		dropAddress(t, ctx, address)
	}
	dropAddress(t, ctx, svc+"/")
	id := strings.NewReplacer(".", "-", ":", "-").Replace(strings.TrimPrefix(svc, "http://"))
	do(t, ctx, chromedp.WaitVisible(chromedp.CSS(fmt.Sprintf(`#nav .nav-item[data-id=%q]`, id))))
	if _, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`location.hash === %q`, "#/"+id))); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`void fetch('/api/layout?drag-check')`)); err != nil {
		t.Fatal(err)
	}
	posts := 0
	for event, err := range events {
		if err != nil {
			t.Fatal(err)
		}
		if event.Request.Method == "POST" && event.Request.URL == url+"/api/pages" {
			posts++
		}
		if event.Request.URL == url+"/api/layout?drag-check" {
			break
		}
	}
	if posts != 1 {
		t.Errorf("dragged console links sent %d add-page requests; want only the one external address", posts)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("plugin folders: %v; want the original plugin and one external page", entries)
	}
	if got, ok := tryFrameText(ctx, "#frames iframe:not([hidden])", "#svc", 5*time.Second); !ok || got != "Hello from the service" {
		t.Fatalf("the external page did not open: %q %v", got, ok)
	}
}

// dropAddress models a URI-list drop that starts outside this document.
func dropAddress(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](fmt.Sprintf(`(() => {
 const dt = new DataTransfer(); dt.setData('text/uri-list',%q);
 for (const type of ['dragover','drop']) document.querySelector('#nav').dispatchEvent(new DragEvent(type,{bubbles:true,cancelable:true,dataTransfer:dt}));
 })()`, address))); err != nil {
		t.Fatal(err)
	}
}
