package husonym_benthos

// A table sync reads one finite page and writes it in batches. Benthos flushes the batch
// it still holds when the input closes, but the input closes only once every message it
// emitted has been acknowledged — and the rows of a partial batch are exactly the ones
// still waiting for it (component/input/async_reader.go). Nothing releases them but the
// batching period, which costs one period per page whatever the page holds.
//
// The input therefore marks the last row of its page and the destination batches on that
// mark, so the tail of a page is written as soon as it is read. The period stays as the
// safety net for the rows the mark never reaches.
const (
	// LastRowOfPageMetaKey is the metadata the SQL input sets on the last row of a page.
	LastRowOfPageMetaKey = "husonym_last_row_of_page"
	// LastRowOfPageCheck is the batching check firing on that row.
	LastRowOfPageCheck = `meta("` + LastRowOfPageMetaKey + `") == "true"`
)
