package casfile

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestParseBase64Payload(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte(`{"name":"hello.txt","size":12,"md5":"abc","sliceMd5":"def","create_time":"1"}`))

	info, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("parse .cas: %v", err)
	}

	if info.Name != "hello.txt" {
		t.Fatalf("unexpected name: %s", info.Name)
	}
	if info.Size != 12 {
		t.Fatalf("unexpected size: %d", info.Size)
	}
	if info.MD5 != "abc" {
		t.Fatalf("unexpected md5: %s", info.MD5)
	}
	if info.SliceMD5 != "def" {
		t.Fatalf("unexpected slice md5: %s", info.SliceMD5)
	}
}

func TestParsePlainJSONPayload(t *testing.T) {
	info, err := Parse([]byte(`{"name":"hello.txt","size":12,"md5":"abc","slice_md5":"def"}`))
	if err != nil {
		t.Fatalf("parse json .cas: %v", err)
	}

	if info.SliceMD5 != "def" {
		t.Fatalf("unexpected slice md5: %s", info.SliceMD5)
	}
}

func TestParseRejectsIncompletePayload(t *testing.T) {
	if _, err := Parse([]byte(`{"name":"hello.txt","size":12,"md5":"abc"}`)); err == nil {
		t.Fatal("expected missing slice md5 error")
	}
}

func TestParseReaderParsesValidPayload(t *testing.T) {
	info, err := ParseReader(strings.NewReader(`{"name":"hello.txt","size":12,"md5":"abc","sliceMd5":"def"}`))
	if err != nil {
		t.Fatalf("parse reader .cas: %v", err)
	}
	if info.Name != "hello.txt" || info.Size != 12 || info.MD5 != "abc" || info.SliceMD5 != "def" {
		t.Fatalf("unexpected info: %#v", info)
	}
}

func TestParseReaderRejectsOversizedMetadata(t *testing.T) {
	content := bytes.Repeat([]byte("x"), MaxMetadataSize+1)
	_, err := ParseReader(bytes.NewReader(content))
	if !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("expected ErrMetadataTooLarge, got %v", err)
	}
}
