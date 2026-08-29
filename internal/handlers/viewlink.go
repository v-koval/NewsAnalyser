package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const viewLinkTTL = 15 * time.Minute

func viewLinkSig(secret []byte, runID string, exp int64) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s|%d", runID, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyViewLink(secret []byte, runID, expStr, sig string, now time.Time) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	want := viewLinkSig(secret, runID, exp)
	return hmac.Equal([]byte(want), []byte(sig))
}

// runViewLink issues a short-lived signed URL for /runs/{id}/view. The run's
// existence is not checked: signing an unknown id is harmless, /view 404s.
func (h *Handlers) runViewLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	exp := time.Now().Add(viewLinkTTL).Unix()
	writeJSON(w, 200, map[string]string{
		"url": fmt.Sprintf("/runs/%s/view?exp=%d&sig=%s", id, exp, viewLinkSig(h.Auth.Secret, id, exp)),
	})
}
