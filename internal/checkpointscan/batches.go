package checkpointscan

import "sort"

// Place is where a blob sits: a ref and a path in its tree, for the message.
type Place struct{ Ref, Path string }

// Blob is one blob a push would send.
type Blob struct {
	Size   int
	Places map[Place]struct{}
}

// Batches orders the blobs by size, smallest first (ties by id), in batches of at most limit
// bytes; a blob larger than limit gets a batch of its own.
func Batches(blobs map[string]Blob, limit int) [][]string {
	ids := make([]string, 0, len(blobs))
	for oid := range blobs {
		ids = append(ids, oid)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := blobs[ids[i]].Size, blobs[ids[j]].Size
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	var out [][]string
	var batch []string
	total := 0
	for _, oid := range ids {
		size := blobs[oid].Size
		if len(batch) > 0 && total+size > limit {
			out = append(out, batch)
			batch, total = nil, 0
		}
		batch = append(batch, oid)
		total += size
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}
