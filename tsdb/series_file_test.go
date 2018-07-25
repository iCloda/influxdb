package tsdb_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/influxdata/influxdb/logger"
	"github.com/influxdata/influxdb/models"
	"github.com/influxdata/influxdb/tsdb"
)

func TestParseSeriesKeyInto(t *testing.T) {
	name := []byte("cpu")
	tags := models.NewTags(map[string]string{"region": "east", "server": "a"})
	key := tsdb.AppendSeriesKey(nil, name, tags)

	dst := make(models.Tags, 0)
	gotName, gotTags := tsdb.ParseSeriesKeyInto(key, dst)

	if !bytes.Equal(gotName, name) {
		t.Fatalf("got %q, expected %q", gotName, name)
	}

	if got, exp := len(gotTags), 2; got != exp {
		t.Fatalf("got tags length %d, expected %d", got, exp)
	} else if got, exp := gotTags, tags; !got.Equal(exp) {
		t.Fatalf("got tags %v, expected %v", got, exp)
	}

	dst = make(models.Tags, 0, 5)
	_, gotTags = tsdb.ParseSeriesKeyInto(key, dst)
	if got, exp := len(gotTags), 2; got != exp {
		t.Fatalf("got tags length %d, expected %d", got, exp)
	} else if got, exp := cap(gotTags), 5; got != exp {
		t.Fatalf("got tags capacity %d, expected %d", got, exp)
	} else if got, exp := gotTags, tags; !got.Equal(exp) {
		t.Fatalf("got tags %v, expected %v", got, exp)
	}

	dst = make(models.Tags, 1)
	_, gotTags = tsdb.ParseSeriesKeyInto(key, dst)
	if got, exp := len(gotTags), 2; got != exp {
		t.Fatalf("got tags length %d, expected %d", got, exp)
	} else if got, exp := gotTags, tags; !got.Equal(exp) {
		t.Fatalf("got tags %v, expected %v", got, exp)
	}
}

// Ensure series file contains the correct set of series.
func TestSeriesFile_Series(t *testing.T) {
	sfile := MustOpenSeriesFile()
	defer sfile.Close()

	series := []Series{
		{Name: []byte("cpu"), Tags: models.NewTags(map[string]string{"region": "east"})},
		{Name: []byte("cpu"), Tags: models.NewTags(map[string]string{"region": "west"})},
		{Name: []byte("mem"), Tags: models.NewTags(map[string]string{"region": "east"})},
	}
	for _, s := range series {
		if _, err := sfile.CreateSeriesListIfNotExists([][]byte{[]byte(s.Name)}, []models.Tags{s.Tags}); err != nil {
			t.Fatal(err)
		}
	}

	// Verify total number of series is correct.
	if n := sfile.SeriesCount(); n != 3 {
		t.Fatalf("unexpected series count: %d", n)
	}

	// Verify all series exist.
	for i, s := range series {
		if seriesID := sfile.SeriesID(s.Name, s.Tags, nil); seriesID.IsZero() {
			t.Fatalf("series does not exist: i=%d", i)
		}
	}

	// Verify non-existent series doesn't exist.
	if sfile.HasSeries([]byte("foo"), models.NewTags(map[string]string{"region": "north"}), nil) {
		t.Fatal("series should not exist")
	}
}

// Ensure series file can be compacted.
func TestSeriesFileCompactor(t *testing.T) {
	sfile := MustOpenSeriesFile()
	defer sfile.Close()

	// Disable automatic compactions.
	for _, p := range sfile.Partitions() {
		p.CompactThreshold = 0
	}

	var names [][]byte
	var tagsSlice []models.Tags
	for i := 0; i < 10000; i++ {
		names = append(names, []byte(fmt.Sprintf("m%d", i)))
		tagsSlice = append(tagsSlice, models.NewTags(map[string]string{"foo": "bar"}))
	}
	if _, err := sfile.CreateSeriesListIfNotExists(names, tagsSlice); err != nil {
		t.Fatal(err)
	}

	// Verify total number of series is correct.
	if n := sfile.SeriesCount(); n != uint64(len(names)) {
		t.Fatalf("unexpected series count: %d", n)
	}

	// Compact in-place for each partition.
	for _, p := range sfile.Partitions() {
		compactor := tsdb.NewSeriesPartitionCompactor()
		if err := compactor.Compact(p); err != nil {
			t.Fatal(err)
		}
	}

	// Verify all series exist.
	for i := range names {
		if seriesID := sfile.SeriesID(names[i], tagsSlice[i], nil); seriesID.IsZero() {
			t.Fatalf("series does not exist: %s,%s", names[i], tagsSlice[i].String())
		}
	}
}

// Series represents name/tagset pairs that are used in testing.
type Series struct {
	Name    []byte
	Tags    models.Tags
	Deleted bool
}

// SeriesFile is a test wrapper for tsdb.SeriesFile.
type SeriesFile struct {
	*tsdb.SeriesFile
}

// NewSeriesFile returns a new instance of SeriesFile with a temporary file path.
func NewSeriesFile() *SeriesFile {
	dir, err := ioutil.TempDir("", "tsdb-series-file-")
	if err != nil {
		panic(err)
	}
	return &SeriesFile{SeriesFile: tsdb.NewSeriesFile(dir)}
}

// MustOpenSeriesFile returns a new, open instance of SeriesFile. Panic on error.
func MustOpenSeriesFile() *SeriesFile {
	f := NewSeriesFile()
	f.Logger = logger.New(os.Stdout)
	if err := f.Open(); err != nil {
		panic(err)
	}
	return f
}

// Close closes the log file and removes it from disk.
func (f *SeriesFile) Close() error {
	defer os.RemoveAll(f.Path())
	return f.SeriesFile.Close()
}