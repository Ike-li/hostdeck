package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// A sidebar dot says whether its address answered by its shape as well as its
// colour, because red and green are one colour to many eyes: an address that
// answered is a filled disc, and one that did not is a red ring with the
// sidebar showing through it. Both keep the same box, so the column of dots
// stays where it was.
func TestASidebarDotSaysUpOrDownByItsShapeAsWellAsItsColour(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(up.Close)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	dir := t.TempDir()
	writePlugin(t, dir, "up", fmt.Sprintf("schema: 1\nname: Up\npage: {proxy: %q}\n", up.URL+"/"))
	writePlugin(t, dir, "down", fmt.Sprintf("schema: 1\nname: Down\npage: {proxy: %q}\n", gone.URL+"/"))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "en")
	do(t, ctx, chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="up"] .dot[data-status="up"]`)),
		chromedp.WaitVisible(chromedp.CSS(`.nav-item[data-id="down"] .dot[data-status="down"]`)))

	result, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
 const issues = [];
 const look = id => {
 const dot = document.querySelector('.nav-item[data-id="' + id + '"] .dot'), s = getComputedStyle(dot), r = dot.getBoundingClientRect();
 return { fill: s.backgroundColor, ring: parseFloat(s.borderTopWidth), ringColour: s.borderTopColor, width: r.width, height: r.height };
 };
 const probe = document.createElement('span');
 probe.style.color = 'var(--error)';
 document.body.append(probe);
 const red = getComputedStyle(probe).color;
 probe.remove();
 const up = look('up'), down = look('down');
 if (up.fill === 'rgba(0, 0, 0, 0)' || up.ring !== 0) issues.push('the dot that answered is not a filled disc: ' + JSON.stringify(up));
 if (down.fill !== 'rgba(0, 0, 0, 0)') issues.push('the dot that did not answer is filled with ' + down.fill);
 if (down.ring < 2 || down.ringColour !== red) issues.push('the dot that did not answer is not a red ring: ' + JSON.stringify(down) + ', red is ' + red);
 if (up.width !== down.width || up.height !== down.height) issues.push('the two dots differ in size: ' + JSON.stringify([up, down]));
 return issues.join('; ') || 'ok';
 })()`))
	if err != nil || result != "ok" {
		t.Fatalf("sidebar dots: %s %v", result, err)
	}
}
