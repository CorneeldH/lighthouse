package checkpointscan

import (
	"testing"
)

func TestBatchesSplitAtLimit(t *testing.T) {
	blobs := map[string]Blob{"a": {Size: 40}, "b": {Size: 50}, "c": {Size: 10}, "d": {Size: 200}}
	eq(t, [][]string{{"c", "a", "b"}, {"d"}}, Batches(blobs, 100))
	eq(t, [][]string{{"c", "a"}, {"b"}, {"d"}}, Batches(blobs, 60))
	empty(t, Batches(nil, 100))
	// Equal sizes order by id; exactly the limit still fits.
	eq(t, [][]string{{"x", "y"}, {"z"}}, Batches(map[string]Blob{"z": {Size: 5}, "y": {Size: 5}, "x": {Size: 5}}, 10))
}

func TestBatchesDefaultLimitIs64MiB(t *testing.T) {
	eq(t, 64*1024*1024, BatchBytes)
	eq(t, 256*1024*1024, HookVolumeLimit)
	blobs := map[string]Blob{"a": {Size: BatchBytes}, "b": {Size: 1}}
	eq(t, [][]string{{"b"}, {"a"}}, Batches(blobs, BatchBytes))
}
