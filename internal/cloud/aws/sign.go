package aws

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func signV4(method, host, uri, region, service, accessKey, secretKey, payloadHash, amzDate, dateStamp string, headers map[string]string) string {
	var hb strings.Builder
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sortStrings(keys)
	signedHeaders := make([]string, 0, len(keys))
	for _, k := range keys {
		hb.WriteString(k)
		hb.WriteByte(':')
		hb.WriteString(strings.TrimSpace(headers[k]))
		hb.WriteByte('\n')
		signedHeaders = append(signedHeaders, k)
	}
	canonicalHeaders := hb.String()
	signedHeadersStr := strings.Join(signedHeaders, ";")

	canonicalRequest := strings.Join([]string{
		method,
		uri,
		"",
		canonicalHeaders,
		signedHeadersStr,
		payloadHash,
	}, "\n")

	credentialScope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256hex(canonicalRequest),
	}, "\n")

	signingKey := hmacChain(secretKey, []string{dateStamp, region, service, "aws4_request"})
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	return "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + credentialScope +
		", SignedHeaders=" + signedHeadersStr + ", Signature=" + signature
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func hmacChain(secret string, keys []string) []byte {
	key := []byte("AWS4" + secret)
	for _, k := range keys {
		key = hmacSHA256(key, k)
	}
	return key
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}
