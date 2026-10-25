package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestALongJobNameAndStatusShareTheCardWithoutSqueezingEachOther(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"count":1,"state":"spawn scheduled · exit 78: EX_CONFIG"}`)
	}))
	t.Cleanup(svc.Close)
	dir := t.TempDir()
	writePlugin(t, dir, "jobs", fmt.Sprintf(`schema: 1
name: {en: Background jobs, zh-CN: 后台任务}
card:
  http: {url: %q}
  value: count
  label: {en: need attention, zh-CN: 需要处理}
  rows:
    - name: com.ajazz.keyboarddriver
      value: state
`, svc.URL))
	url := startConsole(t, dir)
	ctx := openBrowser(t, "zh-CN")
	do(t, ctx, chromedp.Navigate(url), chromedp.WaitVisible(chromedp.CSS(`.card[data-id="jobs"] .row`)))
	for _, lang := range []string{"zh-CN", "en"} {
		do(t, ctx, chromedp.EmulateViewport(1280, 860), chromedp.Click(chromedp.CSS(`button[data-lang="`+lang+`"]`)))
		for _, width := range []int{1280, 2964, 520} {
			do(t, ctx, chromedp.EmulateViewport(int64(width), 860))
			got, err := chromedp.Run(ctx, chromedp.Evaluate[struct {
				NameWidth float64 `json:"nameWidth"`
				RowWidth  float64 `json:"rowWidth"`
				Height    float64 `json:"height"`
				Fits      bool    `json:"fits"`
			}](`(() => {
				const card = document.querySelector('.card[data-id="jobs"]');
				const row = card.querySelector('.row');
				const cr = card.getBoundingClientRect();
				return {
					nameWidth: row.querySelector('.row-name').getBoundingClientRect().width,
					rowWidth: row.getBoundingClientRect().width,
					height: cr.height,
					fits: document.documentElement.scrollWidth <= innerWidth && [...row.children].every(e => {
						const r = e.getBoundingClientRect();
						return r.left >= cr.left && r.right <= cr.right && e.scrollWidth <= e.clientWidth + 1;
					}),
				};
			})()`))
			if err != nil {
				t.Fatal(err)
			}
			if got.NameWidth < got.RowWidth*0.30 || got.Height > 400 || !got.Fits {
				t.Errorf("%s at %dpx: %+v; want readable name and status inside a compact card", lang, width, got)
			}
		}
	}
}
