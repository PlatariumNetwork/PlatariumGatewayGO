package blockchain

// BlockPageQuery selects a bounded slice of block history.
// Full returns every record. Otherwise limit defaults to 50 and page is 1-based.
type BlockPageQuery struct {
	Limit int
	Page  int
	From  *int64
	To    *int64
	Full  bool
}

const DefaultBlockPageLimit = 50

// PageBlocks returns newest blocks first, bounded unless Full is set.
func PageBlocks(blocks []BlockRecord, q BlockPageQuery) []BlockRecord {
	filtered := make([]BlockRecord, 0, len(blocks))
	for _, b := range blocks {
		if q.From != nil && b.BlockNumber < *q.From {
			continue
		}
		if q.To != nil && b.BlockNumber > *q.To {
			continue
		}
		filtered = append(filtered, b)
	}
	for i := 1; i < len(filtered); i++ {
		j := i
		for j > 0 && filtered[j].BlockNumber > filtered[j-1].BlockNumber {
			filtered[j], filtered[j-1] = filtered[j-1], filtered[j]
			j--
		}
	}
	if q.Full {
		return filtered
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultBlockPageLimit
	}
	if limit > 200 {
		limit = 200
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(filtered) {
		return []BlockRecord{}
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end]
}
