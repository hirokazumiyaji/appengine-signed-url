package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iam/v1"
)

type uploadRequest struct {
	ContentType string `json:"contentType"`
}

const alphanum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var (
	projectId          string
	mutex              *sync.Mutex
	rd                 = rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(os.Getpid())))
	serviceAccountName string
	serviceAccountId   string
)

func init() {
	projectId = os.Getenv("GOOGLE_CLOUD_PROJECT")
	serviceAccountName = fmt.Sprintf(
		"%s@appspot.gserviceaccount.com",
		projectId,
	)
	serviceAccountId = fmt.Sprintf(
		"projects/%s/serviceAccounts/%s",
		projectId,
		serviceAccountName,
	)

	mutex = new(sync.Mutex)
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("failed to new storage client: %v", err)
	}
	bucket := client.Bucket(projectId)
	_, err = bucket.Object("index").Attrs(ctx)
	if err == nil {
		return
	}
	if err == storage.ErrBucketNotExist {
		err = bucket.Create(ctx, projectId, nil)
		if err != nil {
			log.Fatalf("failed to create bucket: %v", err)
		}
	}
}

func uniqueId() string {
	var b [20]byte
	mutex.Lock()
	for i := 0; i < len(b); i++ {
		b[i] = alphanum[rd.Intn(len(alphanum))]
	}
	mutex.Unlock()
	return string(b[:])
}

func main() {
	http.HandleFunc("/upload", uploadHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	defer r.Body.Close()
	body := uploadRequest{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	options := &storage.SignedURLOptions{
		GoogleAccessID: serviceAccountName,
		SignBytes: func(bytes []byte) ([]byte, error) {
			return signBytes(ctx, bytes)
		},
		Method:      "PUT",
		Expires:     time.Now().UTC().Add(10 * time.Minute),
		ContentType: body.ContentType,
		Scheme:      storage.SigningSchemeV4,
	}
	ext := ""
	if strings.Contains(body.ContentType, "png") {
		ext = "png"
	} else if strings.Contains(body.ContentType, "jpeg") {
		ext = "jpg"
	} else if strings.Contains(body.ContentType, "jpg") {
		ext = "jpg"
	}
	url, err := storage.SignedURL(
		projectId,
		fmt.Sprintf("images/%s.%s", uniqueId(), ext),
		options,
	)
	if err != nil {
		log.Printf("failed to signed url: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := map[string]string{
		"url": url,
	}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.Printf("failed to encode json: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func signBytes(ctx context.Context, payload []byte) ([]byte, error) {
	client, err := iam.NewService(ctx)
	if err != nil {
		log.Printf("failed to new service: %v", err)
		return nil, err
	}
	res, err := client.Projects.ServiceAccounts.SignBlob(
		serviceAccountId,
		&iam.SignBlobRequest{BytesToSign: base64.StdEncoding.EncodeToString(payload)},
	).Context(ctx).Do()
	if err != nil {
		log.Printf("failed to sign blob: %v", err)
		return nil, err
	}
	return base64.StdEncoding.DecodeString(res.Signature)
}
