package javascript_vm

import "sync"

// Pool keeps sealed runners for the life of the process. Sealing a runner costs
// milliseconds — it builds and freezes every built-in object — while each run starts from
// a global object of its own and leaves nothing behind (see isolation.go): a runner can go
// from one job, one account, one request to the next. What changes from one run to the
// next — the context, the logger, the value API's message — is handed to each run.
//
// The pool holds as many runners as were ever in use at once. A sync.Pool would drop them
// at every garbage collection, and building one in the middle of a Benthos stream delays
// the rows it processes past the flush of their page.
type Pool[T any] struct {
	mu    sync.Mutex
	free  []T
	built int
	build func() (T, error)
}

// NewPool returns a pool building its items with build.
func NewPool[T any](build func() (T, error)) *Pool[T] {
	return &Pool[T]{build: build}
}

// Get returns a free item, or builds one.
func (p *Pool[T]) Get() (T, error) {
	p.mu.Lock()
	if n := len(p.free); n > 0 {
		item := p.free[n-1]
		p.free = p.free[:n-1]
		p.mu.Unlock()
		return item, nil
	}
	p.mu.Unlock()
	return p.newItem()
}

func (p *Pool[T]) newItem() (T, error) {
	item, err := p.build()
	if err == nil {
		p.mu.Lock()
		p.built++
		p.mu.Unlock()
	}
	return item, err
}

// Reserve builds items until the pool has built n of them, for runs that must not wait
// for one.
func (p *Pool[T]) Reserve(n int) error {
	for {
		p.mu.Lock()
		enough := p.built >= n
		p.mu.Unlock()
		if enough {
			return nil
		}
		item, err := p.newItem()
		if err != nil {
			return err
		}
		p.Put(item)
	}
}

// Put gives an item back once its run is over.
func (p *Pool[T]) Put(item T) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.free = append(p.free, item)
}
