package sqlite

import (
 "context"
 "testing"
 "github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestManualEventCommandsAreAtomicAndLocked(t *testing.T) {
 path:=testutil.NewSnapshot(t)
 store,err:=OpenAdminWrites(path);if err!=nil{t.Fatal(err)};defer store.Close()
 e,err:=store.CreateManualEvent(context.Background(),EventInput{ID:"manual-1",Title:"Synthetic",StartsOn:"2026-10-12"});if err!=nil{t.Fatal(err)}
 if e.Provenance!="manual" || !e.ManualLocked || e.TimePrecision!="unknown" {t.Fatalf("event=%+v",e)}
 if _,err=store.UpdateManualEvent(context.Background(),"manual-1",EventInput{Title:"Edited",StartsOn:"2026-10-12",CancellationStatus:"manual_confirmed"});err==nil{t.Fatal("missing cancellation reason accepted")}
 if _,err=store.DeleteManualEvent(context.Background(),"manual-1");err!=nil{t.Fatal(err)}
 if e,err=store.DeleteManualEvent(context.Background(),"missing");err==nil || e.ID!="" {t.Fatal("missing delete accepted")}
}

func TestReadOnlyEventCommandsFailWithoutMutation(t *testing.T) {
 path:=testutil.NewSnapshot(t);store,err:=OpenReadOnly(path);if err!=nil{t.Fatal(err)};defer store.Close()
 if _,err=store.CreateManualEvent(context.Background(),EventInput{Title:"x",StartsOn:"2026-10-12"});err!=ErrReadOnly{t.Fatalf("err=%v",err)}
}
