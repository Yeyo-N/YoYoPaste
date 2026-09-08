package peer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Yeyo-N/YoYoPaste/internal/store"
	"github.com/Yeyo-N/YoYoPaste/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
	"context"
	"tailscale.com/ipn/ipnstate"
)

type stub2 struct{ w *apitype.WhoIsResponse; e error }
func (s *stub2) Status(_ context.Context) (*ipnstate.Status, error){return nil,nil}
func (s *stub2) WhoIs(_ context.Context,_ string)(*apitype.WhoIsResponse,error){
	if s.e!=nil{return nil,s.e}
	return s.w,nil
}

func allow2() *stub2 { return &stub2{w: &apitype.WhoIsResponse{Node:&tailcfg.Node{StableID:"x"}}} }

func TestBlobRangeAndHead(t *testing.T){
	tsnet.SetClient(allow2())
	defer tsnet.ResetClient()
	dir:=t.TempDir()
	st,_:=store.Open(dir)
	defer store.Close(st)
	// create blob file 500 bytes
	content:=make([]byte,500)
	for i:=range content{content[i]=byte(i%256)}
	blobPath:=filepath.Join(dir,"blob1.dat")
	if err:=os.WriteFile(blobPath,content,0600);err!=nil{t.Fatal(err)}
	it:=store.Item{ID:"blob-1",Kind:"file",Mime:"application/octet-stream",Name:"test.dat",Size:500,SHA256:"sha",Origin:"test",Created:time.Now(),BlobPath:blobPath}
	st.Put(it)
	srv:=New(st,"test")
	ts:=httptest.NewServer(srv.Handler())
	defer ts.Close()
	// HEAD
	resp,err:=http.Head(ts.URL+"/v0/blob/blob-1")
	if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=200{t.Fatalf("head %d",resp.StatusCode)}
	if resp.Header.Get("Accept-Ranges")!="bytes"{t.Fatalf("accept-ranges %s",resp.Header.Get("Accept-Ranges"))}
	if resp.ContentLength!=500{t.Fatalf("len %d",resp.ContentLength)}
	// Range
	req,_:=http.NewRequest("GET",ts.URL+"/v0/blob/blob-1",nil)
	req.Header.Set("Range","bytes=100-199")
	resp,err=http.DefaultClient.Do(req)
	if err!=nil{t.Fatal(err)}
	defer resp.Body.Close()
	if resp.StatusCode!=206{t.Fatalf("range status %d",resp.StatusCode)}
	if resp.Header.Get("Content-Range")!="bytes 100-199/500"{t.Fatalf("content-range %s",resp.Header.Get("Content-Range"))}
	body:=make([]byte,100)
	n,_:=resp.Body.Read(body)
	if n!=100{t.Fatalf("read %d",n)}
	for i:=0;i<100;i++{if body[i]!=content[100+i]{t.Fatalf("mismatch at %d",i)}}
	// 404 for unknown id
	resp,_=http.Get(ts.URL+"/v0/blob/notfound")
	if resp.StatusCode!=404{t.Fatalf("expected 404 got %d",resp.StatusCode)}
	resp.Body.Close()
	// path traversal attempt: id with ../ should 404 and not touch filesystem
	resp,_=http.Get(ts.URL+"/v0/blob/..%2Fetc%2Fpasswd")
	// Our cleanID rejects non-alnum, so 404
	if resp.StatusCode!=404{t.Fatalf("traversal expected 404 got %d",resp.StatusCode)}
	resp.Body.Close()
}
