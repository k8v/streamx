package pipe

import "time"

const (
	defaultConcurrency = 5
)

type Pipe[R any] struct {
	source  Source[R]
	stages  []pipeStage[R]
	stopped chan struct{}
	errCh   chan error
}

type Source[R any] func() ([]*R, error)
type Sink[R any] func(*R) error

type pipeStage[R any] interface {
	process(inCh <-chan *R, outCh chan<- *R)
	getBufSize() int
}

func New[R any](source Source[R]) *Pipe[R] {
	return &Pipe[R]{
		source:  source,
		errCh:   make(chan error, 1),
		stopped: make(chan struct{}),
	}
}

func (p *Pipe[R]) Map(fn func(r *R) (*R, error), opts ...SimpleStageOption[R]) {
	p.FanOut(func(in *R) ([]*R, error) {
		out, err := fn(in)
		if err != nil {
			return nil, err
		}

		return []*R{out}, nil
	}, opts...)
}

func (p *Pipe[R]) FanOut(fn func(r *R) ([]*R, error), opts ...SimpleStageOption[R]) {
	stage := &simpleStage[R]{
		fn:          fn,
		concurrency: defaultConcurrency,
		reportError: p.reportError,
		stopped:     p.stopped,
	}

	for _, opt := range opts {
		opt(stage)
	}

	p.stages = append(p.stages, stage)
}

func (p *Pipe[R]) Sink(sink Sink[R]) error {
	return p.SinkWithTimeout(sink, 45*time.Second)
}

func (p *Pipe[R]) SinkWithTimeout(sink Sink[R], timeout time.Duration) error {
	outCh := make(chan *R, p.getBufSize(0))
	go p.startSource(outCh)

	for i, stage := range p.stages {
		inCh := outCh
		outCh = make(chan *R, p.getBufSize(i+1))
		go stage.process(inCh, outCh)
	}

	p.startSink(sink, outCh)
	
	// Use custom timeout instead of p.timeoutCh
	timeoutCh := time.After(timeout)
	select {
	case <-timeoutCh:
		p.Stop()
	case <-p.stopped:
	}

	select {
	case err := <-p.errCh:
		return err
	default:
		return nil
	}
}

func (p *Pipe[R]) Channel(fn func(r *R, stopCh <-chan struct{}, outCh chan<- *R) error) {
	stage := &channelStage[R]{
		fn:          fn,
		concurrency: defaultChannelConcurrency,
		reportError: p.reportError,
		stopped:     p.stopped,
	}

	p.stages = append(p.stages, stage)
}

func (p *Pipe[R]) Stop() {
	select {
	case <-p.stopped:
	default:
		close(p.stopped)
	}
}

func (p *Pipe[R]) Batch(fn func([]*R) ([]*R, error), opts ...BatchStageOption[R]) {
	stage := &batchStage[R]{
		fn:          fn,
		workerSize:  defaultWorkerSize,
		batchSize:   defaultBatchSize,
		reportError: p.reportError,
		stoped:      p.stopped,
		batchCh:     make(chan []*R),
	}

	for _, opt := range opts {
		opt(stage)
	}

	p.stages = append(p.stages, stage)
}


func (p *Pipe[R]) Filter(fn func(r *R) bool, opts ...SimpleStageOption[R]) {
	p.FanOut(func(in *R) ([]*R, error) {
		ok := fn(in)
		if ok {
			return []*R{in}, nil
		}

		return nil, nil
	}, opts...)
}

func (p *Pipe[R]) Stage(stage pipeStage[R]) {
	p.stages = append(p.stages, stage)
}

func (p *Pipe[R]) startSource(outCh chan<- *R) {
	defer close(outCh)
	records, err := p.source()
	if err != nil {
		p.reportError(err)
		return
	}

	SendRecords(records, outCh, p.stopped)
}

func (p *Pipe[R]) startSink(sink Sink[R], inCh <-chan *R) {
	go func() {
		for record := range inCh {
			err := sink(record)
			if err != nil {
				p.reportError(err)
			}
		}
		p.Stop()
	}()
}

func (p *Pipe[R]) reportError(err error) {
	select {
	case <-p.stopped:
	case p.errCh <- err:
		p.Stop()
	default:
	}
}

func (p *Pipe[R]) getBufSize(index int) int {
	if index >= len(p.stages) {
		return 0
	}

	return p.stages[index].getBufSize()
}
