package utils

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadJSONRequiresOneCompleteValue(t *testing.T) {
	for _, body := range []string{`{} {}`, `{} garbage`, `{"value":`, `{"value":"x"} {`} {
		_, err := ReadJson[map[string]any](httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if err == nil {
			t.Fatalf("accepted invalid body %q", body)
		}
	}
	for _, body := range []string{"{} \n\t", `{"value":"ok"}`} {
		if _, err := ReadJson[map[string]any](httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadJSONBoundsUnknownLengthAndTrailingWhitespace(t *testing.T) {
	for _, prefix := range []string{`{"value":"`, `{} `} {
		body := io.MultiReader(strings.NewReader(prefix), strings.NewReader(strings.Repeat(" ", maxJSONBody)))
		r := httptest.NewRequest(http.MethodPost, "/", body)
		_, err := ReadJson[map[string]any](r)
		var limitErr *http.MaxBytesError
		if !errors.As(err, &limitErr) {
			t.Fatalf("expected size limit, got %v", err)
		}
	}
}
