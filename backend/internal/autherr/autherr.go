// Package autherr berisi alasan error AuthService yang dibaca gateway
// (langkah 26). Dipisah dari package auth supaya gateway tidak ikut
// membawa driver Oracle (godror) yang di-import package auth.
//
// Alasan dikirim sebagai detail ErrorInfo di status gRPC (lihat
// https://google.aip.dev/193), jadi gateway tidak perlu mencocokkan teks
// pesan error yang bisa berubah.
package autherr

// Domain mengisi ErrorInfo.Domain.
const Domain = "auth.ice-stream"

// Alasan FAILED_PRECONDITION dari LoginWithIdentity.
const (
	ReasonEmailNotVerified      = "EMAIL_NOT_VERIFIED"
	ReasonEmailNeedsPassword    = "EMAIL_NEEDS_PASSWORD"
	ReasonProviderAlreadyLinked = "PROVIDER_ALREADY_LINKED"
)
