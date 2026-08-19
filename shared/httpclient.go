package shared

import (
	"io"
	"net/http"
	"time"
)

// DefaultHTTPTimeout is the default timeout for outbound HTTP calls.
const DefaultHTTPTimeout = 30 * time.Second

// HTTPClient is a reusable client with sane defaults.
var HTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}

// Do executes an HTTP request and returns status, body, error.
func Do(req *http.Request) (int, []byte, error) {
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// ReadAll reads from r until EOF or error, returning accumulated bytes.
func ReadAll(r io.Reader) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	for {
		tmp := make([]byte, 4096)
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return buf, err
		}
		if n == 0 {
			break
		}
	}
	return buf, nil
}
