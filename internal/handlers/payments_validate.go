package handlers

var validMethods = map[string]bool{"pos": true, "cash": true}

// validateProofURL returns the error message for an invalid proof URL, or
// "" — which also covers the empty value, meaning "no proof attached".
func validateProofURL(u string) string {
	if u == "" {
		return ""
	}
	if len(u) > maxURLLen {
		return "proof_url is too long"
	}
	if !validURL(u) {
		return "invalid proof_url"
	}
	return ""
}
