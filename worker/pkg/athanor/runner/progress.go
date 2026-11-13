package runner

import (
	"sync/atomic"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

// Progress counts what a page has done so far, for whoever watches it from another goroutine:
// the rows read from the source, the rows written to the destination, and whether the page is
// writing a batch right now or waiting for the source. Its zero value is ready to use.
type Progress struct {
	read    atomic.Int64
	written atomic.Int64
	writing atomic.Bool
}

// ProgressSnapshot is what a Progress says at one moment.
type ProgressSnapshot struct {
	RowsRead    int64
	RowsWritten int64
	// Writing says that the page is writing a batch to the destination; otherwise it waits
	// for the source, or works on what it read.
	Writing bool `json:"-"`
}

// Snapshot reads the counters. A nil Progress reads as nothing done.
func (p *Progress) Snapshot() ProgressSnapshot {
	if p == nil {
		return ProgressSnapshot{}
	}
	return ProgressSnapshot{
		RowsRead:    p.read.Load(),
		RowsWritten: p.written.Load(),
		Writing:     p.writing.Load(),
	}
}

func (p *Progress) rowRead() {
	if p != nil {
		p.read.Add(1)
	}
}

// Wrap counts on p what inner writes, as the writers of a page are: the time each batch takes,
// and the rows that reach the destination.
func (p *Progress) Wrap(inner sqlio.RowWriter) sqlio.RowWriter {
	return p.batchWriter(p.rowsWriter(inner))
}

// batchWriter marks the time a batch takes to write, the checks of its parents and the
// translation of its keys included: they all query the destination.
func (p *Progress) batchWriter(inner sqlio.RowWriter) sqlio.RowWriter {
	if p == nil {
		return inner
	}
	return &progressWriter{inner: inner, onWrite: func(rows [][]any, write func() error) error {
		p.writing.Store(true)
		defer p.writing.Store(false)
		return write()
	}}
}

// rowsWriter counts the rows that reach the destination: those the checks left in.
func (p *Progress) rowsWriter(inner sqlio.RowWriter) sqlio.RowWriter {
	if p == nil {
		return inner
	}
	return &progressWriter{inner: inner, onWrite: func(rows [][]any, write func() error) error {
		if err := write(); err != nil {
			return err
		}
		p.written.Add(int64(len(rows)))
		return nil
	}}
}

type progressWriter struct {
	inner   sqlio.RowWriter
	onWrite func(rows [][]any, write func() error) error
}

func (w *progressWriter) WriteBatch(columns []string, rows [][]any) error {
	return w.onWrite(rows, func() error { return w.inner.WriteBatch(columns, rows) })
}
