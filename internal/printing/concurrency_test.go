package printing

import (
	"sync"
	"testing"

	"openscale/internal/domain"
)

// TestTwoLabelsRenderAtOnceWithoutCorruptingEachOther.
//
// The station panicked in production on `GET /admin/api/label/preview.png`, four times in
// seven seconds, with three different messages — « index out of range [20] with length
// 0 », « index out of range [16] », « slice bounds out of range [406:341] » — all inside
// golang.org/x/image, and all on the SAME *sfnt.Font pointer from different goroutines.
//
// Neither sfnt.Font nor opentype.Face is safe for concurrent use: each keeps a scratch
// buffer and a vector rasterizer that it reuses from call to call. The Library's mutex
// protected the MAP of memoised faces and nothing else — the faces themselves were handed
// out and then drawn with, outside the lock, by every HTTP goroutine at once.
//
// It takes two overlapping requests, which is one volunteer clicking twice: the preview
// refreshes on every change of a template field. The same collision reaches the PRINT
// path, where two goroutines rasterising at once would corrupt the image that goes on a
// customer's bag.
func TestTwoLabelsRenderAtOnceWithoutCorruptingEachOther(t *testing.T) {
	template := domain.IdenticalTemplate()
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	// Eight goroutines, sixteen renders each: enough for the scratch buffers to overlap
	// on any machine. Without the fix this panics inside x/image within a few rounds.
	const goroutines, rounds = 8, 16

	var wg sync.WaitGroup
	failures := make(chan error, goroutines*rounds)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				if _, err := Rasterize(&template, label, domain.LocaleFrench, RenderOptions{}); err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)

	for err := range failures {
		t.Fatalf("rendu simultané : %v", err)
	}
}
