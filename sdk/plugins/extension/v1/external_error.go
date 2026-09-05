package extensionv1

import (
	"unicode/utf8"

	"google.golang.org/grpc/status"
)

const (
	// externalErrorLimit bounds extension-originated error text at contract ingress.
	externalErrorLimit = 65536
	// externalErrorTruncation marks the loss of an oversized external error suffix.
	externalErrorTruncation = "\n[external error text truncated]"
)

// mapPeerStreamError applies the external text bound at received gRPC status ingress.
func mapPeerStreamError(err error) error {
	peer, present := status.FromError(err)
	if !present || len(peer.Message()) <= externalErrorLimit {
		return mapStreamError(err)
	}
	// Do not retain an unbounded external status as an unwrap cause after ingress normalization.
	bounded := peer.Proto()
	bounded.Message = boundExternalError(bounded.GetMessage())
	return status.FromProto(bounded).Err()
}

// boundExternalError retains the largest complete UTF-8 prefix within the ingress byte limit.
func boundExternalError(text string) string {
	if len(text) <= externalErrorLimit {
		return text
	}
	end := externalErrorLimit - len(externalErrorTruncation)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + externalErrorTruncation
}
