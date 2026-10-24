package repository

import (
 "testing"
 "github.com/jackc/pgx/v5/pgtype"
)
func TestNormalizeNumeric(t *testing.T){
 var n pgtype.Numeric
 if e:=n.Scan("37.125");e!=nil{t.Fatal(e)}
 if got,ok:=normalizeValue(n).(float64);!ok||got!=37.125{t.Fatalf("numeric not normalized: %T %v",normalizeValue(n),normalizeValue(n))}
}
func TestMemoryNestedRollback(t *testing.T){
 s:=NewMemoryStore();ctx:=t.Context()
 if e:=s.Write(ctx,func(tx WebTx)error{return tx.Put("samples",Record{"id":"a","meta":map[string]any{"secret":"original"}})});e!=nil{t.Fatal(e)}
 _ = s.Read(ctx,func(tx WebTx)error{r,e:=tx.Get("samples","a");if e!=nil{return e};r["meta"].(map[string]any)["secret"]="changed";return nil})
 _ = s.Read(ctx,func(tx WebTx)error{r,e:=tx.Get("samples","a");if e!=nil{return e};if r["meta"].(map[string]any)["secret"]!="original"{t.Fatal("read leaked mutation")};return nil})
}
