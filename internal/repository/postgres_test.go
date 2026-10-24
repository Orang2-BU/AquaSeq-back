package repository

import (
	"context"
	"os"
	"testing"
)

func TestPostgresPersistence(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("DATABASE_URL_TEST tidak diatur")
	}
	s, e := OpenPostgres(context.Background(), url, 4)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	id := "83d23cac-86d0-4ca4-bc49-2055793b53d7"
	if e = s.Write(context.Background(), func(tx WebTx) error {
		return tx.Put("users", Record{"id": id, "email": "repository-test@example.test", "full_name": "Uji Persistensi", "password_hash": "hash"})
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.Read(context.Background(), func(tx WebTx) error {
		r, e := tx.Get("users", id)
		if e == nil && r["email"] != "repository-test@example.test" {
			t.Fatal(r)
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
