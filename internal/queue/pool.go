package queue

import "sync"

// Job is a unit of work submitted to the pool. Run does the actual
// (expensive) container execution; the pool's only job is to bound how many
// of these happen at once.
type Job struct {
	Run func()
}

// Pool caps concurrent execution at `workers` goroutines. This is the whole
// cost-control story for a single VM: without it, N simultaneous submissions
// would spawn N Docker containers and the host would thrash or OOM. With it,
// extra submissions simply queue in the buffered channel instead of piling
// onto the machine.
type Pool struct {
	jobs chan Job
	wg   sync.WaitGroup
}

func NewPool(workers, queueSize int) *Pool {
	p := &Pool{jobs: make(chan Job, queueSize)}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for job := range p.jobs {
		job.Run()
	}
}

// Submit blocks if the queue is full, which is intentional backpressure
// rather than an error — callers running this behind an HTTP handler will
// simply have the request take longer under heavy load rather than the
// server falling over.
func (p *Pool) Submit(j Job) {
	p.jobs <- j
}

func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
}
