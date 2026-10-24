package repository

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryTransactionRollback(t *testing.T) {
	s := NewMemoryStore()
	err := s.Write(context.Background(), func(tx WebTx) error {
		if err := tx.Put("users", Record{"id": "a", "email": "a@example.test"}); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("rollback missing")
	}
	if err := s.Read(context.Background(), func(tx WebTx) error {
		rows, e := tx.List("users")
		if len(rows) != 0 {
			t.Fatal("partial write")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
