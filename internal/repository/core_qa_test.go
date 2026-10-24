package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func qaID() string {
	return fmt.Sprintf("%08x-%04x-4000-8000-%012x", uint32(time.Now().UnixNano()), uint16(time.Now().UnixNano()>>32), uint64(time.Now().UnixNano())&0xffffffffffff)
}

func TestCoreMemoryIgnoredPutAborts(t *testing.T) {
	s := NewMemoryStore()
	err := s.Write(context.Background(), func(tx WebTx) error {
		if err := tx.Put("users", Record{"id": "good"}); err != nil {
			return err
		}
		_ = tx.Put("users", Record{})
		return nil
	})
	if err == nil {
		t.Fatal("ignored Put error committed transaction")
	}
	if err := s.Read(context.Background(), func(tx WebTx) error {
		_, err := tx.Get("users", "good")
		if !errors.Is(err, ErrNotFound) {
			t.Fatal("partial commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCoreIgnoredPutRollsBack(t *testing.T) {
	for name, store := range coreStores(t) {
		t.Run(name, func(t *testing.T) {
			id := qaID()
			err := store.Write(context.Background(), func(tx WebTx) error {
				if err := tx.Put("users", Record{"id": id, "email": id + "@example.test", "full_name": "QA rollback", "password_hash": "test"}); err != nil {
					return err
				}
				_ = tx.Put("users", Record{})
				return nil
			})
			if err == nil {
				t.Fatal("ignored Put error committed")
			}
			if err = store.Read(context.Background(), func(tx WebTx) error {
				_, e := tx.Get("users", id)
				if !errors.Is(e, ErrNotFound) {
					t.Fatal("partial commit")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCoreFindEqual(t *testing.T) {
	for name, store := range coreStores(t) {
		t.Run(name, func(t *testing.T) {
			id := qaID()
			email := id + "' OR true --@example.test"
			if err := store.Write(context.Background(), func(tx WebTx) error {
				return tx.Put("users", Record{"id": id, "email": email, "full_name": "QA", "password_hash": "test"})
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.Read(context.Background(), func(tx WebTx) error {
				rows, err := tx.FindEqual("users", "email", email)
				if err != nil {
					return err
				}
				if len(rows) != 1 || rows[0]["id"] != id {
					t.Fatal("equality lookup failed")
				}
				if _, err = tx.FindEqual("users; DROP TABLE users", "email", email); err == nil {
					t.Fatal("table not rejected")
				}
				if _, err = tx.FindEqual("users", "email OR true", email); err == nil {
					t.Fatal("column not rejected")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func coreStores(t *testing.T) map[string]WebStore {
	stores := map[string]WebStore{"memory": NewMemoryStore()}
	if url := os.Getenv("DATABASE_URL_TEST"); url != "" {
		s, err := OpenPostgres(context.Background(), url, 6)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		stores["postgres"] = s
	}
	return stores
}

func TestCorePostgresIndependentWrites(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	s, err := OpenPostgres(context.Background(), url, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.Write(context.Background(), func(tx WebTx) error {
			if err := tx.Put("users", Record{"id": qaID(), "email": qaID() + "@example.test", "full_name": "QA", "password_hash": "test"}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = s.Write(ctx, func(tx WebTx) error {
		return tx.Put("users", Record{"id": qaID(), "email": qaID() + "@example.test", "full_name": "QA", "password_hash": "test"})
	})
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatalf("independent writes serialized: %v", err)
	}
}
