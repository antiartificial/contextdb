package client_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/antiartificial/contextdb/internal/namespace"
	"github.com/antiartificial/contextdb/pkg/client"
)

func TestNamespace_ConcurrentWriteRetrieveSoak(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := client.MustOpen(client.Options{Mode: client.ModeEmbedded})
	defer db.Close()
	ns := db.Namespace("test:soak", namespace.ModeGeneral)

	duration := 250 * time.Millisecond
	if testing.Short() {
		duration = 100 * time.Millisecond
	}
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	errs := make(chan error, 64)

	for worker := 0; worker < 4; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				if _, err := ns.Write(ctx, client.WriteRequest{
					Content:  fmt.Sprintf("soak worker %d write %d", worker, i),
					SourceID: fmt.Sprintf("soak:%d", worker),
					Labels:   []string{"Soak"},
					Vector:   vec8((worker + i) % 8),
				}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for worker := 0; worker < 2; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				if _, err := ns.Retrieve(ctx, client.RetrieveRequest{
					Text:   "soak",
					Vector: vec8(worker),
					TopK:   5,
				}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
