package httpapi

import("net/http/httptest";"testing";"time")
func TestMediaFreshETag(t *testing.T){r:=httptest.NewRequest("GET","/",nil);r.Header.Set("If-None-Match",`"abc"`);if !mediaFresh(r,"abc",time.Time{}){t.Fatal("etag not matched")}}
