package index

import (
	"math/rand"
	"restic"
	"restic/backend"
	"restic/backend/local"
	"restic/pack"
	"restic/repository"
	"testing"
	"time"
)

var (
	snapshotTime = time.Unix(1470492820, 207401672)
	snapshots    = 3
	depth        = 3
)

func createFilledRepo(t testing.TB, snapshots int, dup float32) (*repository.Repository, func()) {
	repo, cleanup := repository.TestRepository(t)

	for i := 0; i < 3; i++ {
		restic.TestCreateSnapshot(t, repo, snapshotTime.Add(time.Duration(i)*time.Second), depth, dup)
	}

	return repo, cleanup
}

func validateIndex(t testing.TB, repo *repository.Repository, idx *Index) {
	for id := range repo.List(backend.Data, nil) {
		if _, ok := idx.Packs[id]; !ok {
			t.Errorf("pack %v missing from index", id.Str())
		}
	}
}

func TestIndexNew(t *testing.T) {
	repo, cleanup := createFilledRepo(t, 3, 0)
	defer cleanup()

	idx, err := New(repo)
	if err != nil {
		t.Fatalf("New() returned error %v", err)
	}

	if idx == nil {
		t.Fatalf("New() returned nil index")
	}

	validateIndex(t, repo, idx)
}

func TestIndexLoad(t *testing.T) {
	repo, cleanup := createFilledRepo(t, 3, 0)
	defer cleanup()

	loadIdx, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() returned error %v", err)
	}

	if loadIdx == nil {
		t.Fatalf("Load() returned nil index")
	}

	validateIndex(t, repo, loadIdx)

	newIdx, err := New(repo)
	if err != nil {
		t.Fatalf("New() returned error %v", err)
	}

	if len(loadIdx.Packs) != len(newIdx.Packs) {
		t.Errorf("number of packs does not match: want %v, got %v",
			len(loadIdx.Packs), len(newIdx.Packs))
	}

	validateIndex(t, repo, newIdx)

	for packID, packNew := range newIdx.Packs {
		packLoad, ok := loadIdx.Packs[packID]

		if !ok {
			t.Errorf("loaded index does not list pack %v", packID.Str())
			continue
		}

		if len(packNew.Entries) != len(packLoad.Entries) {
			t.Errorf("  number of entries in pack %v does not match: %d != %d\n  %v\n  %v",
				packID.Str(), len(packNew.Entries), len(packLoad.Entries),
				packNew.Entries, packLoad.Entries)
			continue
		}

		for _, entryNew := range packNew.Entries {
			found := false
			for _, entryLoad := range packLoad.Entries {
				if !entryLoad.ID.Equal(entryNew.ID) {
					continue
				}

				if entryLoad.Type != entryNew.Type {
					continue
				}

				if entryLoad.Offset != entryNew.Offset {
					continue
				}

				if entryLoad.Length != entryNew.Length {
					continue
				}

				found = true
				break
			}

			if !found {
				t.Errorf("blob not found in loaded index: %v", entryNew)
			}
		}
	}
}

func openRepo(t testing.TB, dir, password string) *repository.Repository {
	b, err := local.Open(dir)
	if err != nil {
		t.Fatalf("open backend %v failed: %v", dir, err)
	}

	r := repository.New(b)
	err = r.SearchKey(password)
	if err != nil {
		t.Fatalf("unable to open repo with password: %v", err)
	}

	return r
}

func BenchmarkIndexNew(b *testing.B) {
	repo, cleanup := createFilledRepo(b, 3, 0)
	defer cleanup()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx, err := New(repo)

		if err != nil {
			b.Fatalf("New() returned error %v", err)
		}

		if idx == nil {
			b.Fatalf("New() returned nil index")
		}
	}
}

func TestIndexDuplicateBlobs(t *testing.T) {
	repo, cleanup := createFilledRepo(t, 3, 0.01)
	defer cleanup()

	idx, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}

	dups := idx.DuplicateBlobs()
	if len(dups) == 0 {
		t.Errorf("no duplicate blobs found")
	}
	t.Logf("%d packs, %d unique blobs", len(idx.Packs), len(idx.Blobs))

	packs := idx.PacksForBlobs(dups)
	if len(packs) == 0 {
		t.Errorf("no packs with duplicate blobs found")
	}
	t.Logf("%d packs with duplicate blobs", len(packs))
}

func loadIndex(t testing.TB, repo *repository.Repository) *Index {
	idx, err := Load(repo)
	if err != nil {
		t.Fatalf("Load() returned error %v", err)
	}

	return idx
}

func TestIndexSave(t *testing.T) {
	repo, cleanup := createFilledRepo(t, 3, 0)
	defer cleanup()

	idx := loadIndex(t, repo)

	packs := make(map[backend.ID][]pack.Blob)
	for id := range idx.Packs {
		if rand.Float32() < 0.5 {
			packs[id] = idx.Packs[id].Entries
		}
	}

	t.Logf("save %d/%d packs in a new index\n", len(packs), len(idx.Packs))

	id, err := Save(repo, packs, idx.IndexIDs.List())
	if err != nil {
		t.Fatalf("unable to save new index: %v", err)
	}

	t.Logf("new index saved as %v", id.Str())

	for id := range idx.IndexIDs {
		t.Logf("remove index %v", id.Str())
		err = repo.Backend().Remove(backend.Index, id.String())
		if err != nil {
			t.Errorf("error removing index %v: %v", id, err)
		}
	}

	idx2 := loadIndex(t, repo)
	t.Logf("load new index with %d packs", len(idx2.Packs))

	if len(idx2.Packs) != len(packs) {
		t.Errorf("wrong number of packs in new index, want %d, got %d", len(packs), len(idx2.Packs))
	}

	for id := range packs {
		if _, ok := idx2.Packs[id]; !ok {
			t.Errorf("pack %v is not contained in new index", id.Str())
		}
	}

	for id := range idx2.Packs {
		if _, ok := packs[id]; !ok {
			t.Errorf("pack %v is not contained in new index", id.Str())
		}
	}
}
