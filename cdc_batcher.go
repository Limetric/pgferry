package main

// CDCBatcher accumulates CDC events and flushes them as batches
// when the size threshold is reached or on manual flush.
type CDCBatcher struct {
	batch   []*CDCEvent
	maxSize int
	lastPos CDCPosition
}

func newCDCBatcher(maxSize int) *CDCBatcher {
	return &CDCBatcher{
		batch:   make([]*CDCEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add appends an event. Returns the full batch if the size threshold is reached, else nil.
func (b *CDCBatcher) Add(ev *CDCEvent) []*CDCEvent {
	b.batch = append(b.batch, ev)
	b.lastPos = ev.Position
	if len(b.batch) >= b.maxSize {
		return b.Flush()
	}
	return nil
}

// Flush returns the current batch and resets. Returns nil if empty.
func (b *CDCBatcher) Flush() []*CDCEvent {
	if len(b.batch) == 0 {
		return nil
	}
	batch := b.batch
	b.batch = make([]*CDCEvent, 0, b.maxSize)
	return batch
}

// Position returns the binlog position of the last event added.
func (b *CDCBatcher) Position() CDCPosition {
	return b.lastPos
}

// Len returns the number of buffered events.
func (b *CDCBatcher) Len() int {
	return len(b.batch)
}
