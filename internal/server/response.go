package server

import "os"

// ResponseEncoder transforms the outbound JSON body of every API response,
// letting different teams wrap payloads to match their own conventions
// (e.g. {"code":..., "data":...} vs raw data). Implement one and register it
// via SetResponseEncoder, or pick a built-in with GPI_RESPONSE_FORMAT.
type ResponseEncoder interface {
	// EncodeSuccess wraps a successful response body.
	EncodeSuccess(status int, data any) any
	// EncodeError wraps an error response body.
	EncodeError(status int, err error) any
}

// rawEncoder is the default: success returns the payload as-is, errors return
// {"error": "..."}.
type rawEncoder struct{}

func (rawEncoder) EncodeSuccess(_ int, data any) any { return data }

func (rawEncoder) EncodeError(_ int, err error) any {
	return map[string]string{"error": err.Error()}
}

// EnvelopeEncoder wraps every response as {"code": status, "message": "...",
// "data": ...}. Field names are configurable via EnvelopeConfig.
type EnvelopeEncoder struct {
	CodeField    string
	MessageField string
	DataField    string
}

// EnvelopeConfig names the fields of an EnvelopeEncoder.
type EnvelopeConfig struct {
	Code    string
	Message string
	Data    string
}

// NewEnvelopeEncoder returns an envelope encoder using the given field names
// (defaults: "code", "message", "data").
func NewEnvelopeEncoder(cfg EnvelopeConfig) *EnvelopeEncoder {
	if cfg.Code == "" {
		cfg.Code = "code"
	}
	if cfg.Message == "" {
		cfg.Message = "message"
	}
	if cfg.Data == "" {
		cfg.Data = "data"
	}
	return &EnvelopeEncoder{CodeField: cfg.Code, MessageField: cfg.Message, DataField: cfg.Data}
}

func (e *EnvelopeEncoder) EncodeSuccess(status int, data any) any {
	out := map[string]any{e.CodeField: status, e.MessageField: "ok"}
	if data != nil {
		out[e.DataField] = data
	}
	return out
}

func (e *EnvelopeEncoder) EncodeError(status int, err error) any {
	return map[string]any{
		e.CodeField:    status,
		e.MessageField: err.Error(),
		e.DataField:    nil,
	}
}

// ResponseFormat is the built-in encoder selected by GPI_RESPONSE_FORMAT.
type ResponseFormat string

const (
	// FormatRaw is the default raw encoder.
	FormatRaw ResponseFormat = "raw"
	// FormatEnvelope uses the envelope encoder with default field names.
	FormatEnvelope ResponseFormat = "envelope"
)

// NewResponseEncoder returns the built-in encoder for a format name.
func NewResponseEncoder(format string) ResponseEncoder {
	switch ResponseFormat(format) {
	case FormatEnvelope:
		return NewEnvelopeEncoder(EnvelopeConfig{})
	default:
		return rawEncoder{}
	}
}

// responseFormatFromEnv selects a built-in encoder from GPI_RESPONSE_FORMAT.
func responseFormatFromEnv() ResponseEncoder {
	return NewResponseEncoder(os.Getenv("GPI_RESPONSE_FORMAT"))
}
