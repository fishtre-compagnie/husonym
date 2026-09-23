package utils

import "encoding/json"

// LenientBool is a boolean read from a payload an identity provider writes, which reads
// anything it does not understand as false.
//
// It exists because of where such a payload is decoded. The JWT validator unmarshals the
// whole token into one struct and rejects the token on any error, and the userinfo
// response is unmarshalled the same way: a single claim spelled in an unexpected shape
// would fail the sign-in of every user of that deployment. A claim that only decides what
// a page displays must never be able to do that.
//
// So this type never fails. A JSON boolean is itself; the strings "true" and "false" are
// what they say; anything else -- a number, an object, a spelling nobody anticipated -- is
// false, which is the absence of an assertion rather than the denial of one. Callers that
// need to tell "absent" from "asserted false" cannot use this type.
type LenientBool bool

func (b *LenientBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = LenientBool(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*b = asString == "true"
		return nil
	}

	*b = false
	return nil
}
