// Package protosecret encrypts, decrypts and redacts the fields a message marks secret
// (mgmt.v1alpha1.secret).
//
// It is the one place that reads the annotation and walks a message, so that a new setting
// with a secret in it is a new field, not a new piece of encryption code. Fields are
// handled one by one rather than the message as a whole: what is not a secret stays
// readable in the database for diagnosis, and only the secret is opaque.
package protosecret

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sym_encrypt "github.com/fishtre-compagnie/husonym/internal/encrypt/sym"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// fingerprintBytes is how much of the digest a fingerprint shows. Enough to tell two
// secrets apart, far too little to walk back to one.
const fingerprintBytes = 4

// Encrypt returns a copy of msg whose secret fields hold their ciphertext.
func Encrypt[T proto.Message](encryptor sym_encrypt.Interface, msg T) (T, error) {
	return transform(msg, func(path, value string) (string, error) {
		out, err := encryptor.Encrypt(value)
		if err != nil {
			return "", fmt.Errorf("unable to encrypt %s: %w", path, err)
		}
		return out, nil
	})
}

// Decrypt returns a copy of msg whose secret fields hold their plaintext.
func Decrypt[T proto.Message](encryptor sym_encrypt.Interface, msg T) (T, error) {
	return transform(msg, func(path, value string) (string, error) {
		out, err := encryptor.Decrypt(value)
		if err != nil {
			return "", fmt.Errorf("unable to decrypt %s: %w", path, err)
		}
		return out, nil
	})
}

// Redact returns a copy of msg with its secret fields cleared, and the fingerprint of each
// one it cleared, by the path of its field ("anonymization_consistency.derivation_key").
//
// It is given a message in clear: a fingerprint is of the secret itself, so that the same
// secret fingerprints the same in two deployments — which is what anyone reading one wants
// to know. Fingerprinting the ciphertext would tell nothing, as encrypting twice never
// gives the same bytes.
func Redact[T proto.Message](msg T) (redacted T, fingerprintsByField map[string]string, err error) {
	fingerprintsByField = map[string]string{}
	redacted, err = transform(msg, func(path, value string) (string, error) {
		fingerprintsByField[path] = Fingerprint(value)
		return "", nil
	})
	return redacted, fingerprintsByField, err
}

// Fingerprint is the first bytes of a digest of a secret, in hex.
func Fingerprint(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:fingerprintBytes])
}

// transform copies msg and applies fn to every field it marks secret. An empty result
// clears the field, which is how Redact leaves no secret behind.
func transform[T proto.Message](msg T, fn func(path, value string) (string, error)) (T, error) {
	if !msg.ProtoReflect().IsValid() {
		// A nil message carries no secret; handing back a message the caller did not
		// give would be a surprise of its own.
		return msg, nil
	}
	out, ok := proto.Clone(msg).(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("unable to copy %s", msg.ProtoReflect().Descriptor().FullName())
	}
	if err := walk(out.ProtoReflect(), "", fn); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// walk applies fn to the secret fields of m and of the messages it carries. Range only
// visits the fields that are set, so the variant a oneof does not hold is never touched.
func walk(m protoreflect.Message, prefix string, fn func(path, value string) (string, error)) error {
	var err error
	m.Range(func(fd protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		path := prefix + string(fd.Name())
		if isSecret(fd) {
			err = applySecret(m, fd, path, fn)
			return err == nil
		}
		if fd.Kind() != protoreflect.MessageKind {
			return true
		}
		if fd.IsList() || fd.IsMap() {
			// A message held in a list or a map is not descended into: the caller hands
			// this package one setting, not a response carrying several. Rather than
			// leave a secret in clear without a word, say so.
			err = refuseSecretOutOfReach(fd, path)
			return err == nil
		}
		err = walk(value.Message(), path+".", fn)
		return err == nil
	})
	return err
}

// refuseSecretOutOfReach fails when a repeated or map field leads to a secret, so that
// adding one there is caught at the first call rather than found in a database.
func refuseSecretOutOfReach(fd protoreflect.FieldDescriptor, path string) error {
	message := fd.Message()
	if fd.IsMap() {
		if fd.MapValue().Kind() != protoreflect.MessageKind {
			return nil
		}
		message = fd.MapValue().Message()
	}
	if !leadsToSecret(message, map[protoreflect.FullName]bool{}) {
		return nil
	}
	return fmt.Errorf(
		"%s holds a secret in a list or a map, which this package does not walk into: "+
			"hand it each one on its own, or teach walk to descend", path,
	)
}

// leadsToSecret says whether a secret can be reached from this message.
func leadsToSecret(
	message protoreflect.MessageDescriptor,
	seen map[protoreflect.FullName]bool,
) bool {
	if seen[message.FullName()] {
		return false
	}
	seen[message.FullName()] = true

	fields := message.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if isSecret(field) {
			return true
		}
		if field.Kind() != protoreflect.MessageKind {
			continue
		}
		next := field.Message()
		if field.IsMap() {
			if field.MapValue().Kind() != protoreflect.MessageKind {
				continue
			}
			next = field.MapValue().Message()
		}
		if leadsToSecret(next, seen) {
			return true
		}
	}
	return false
}

// applySecret replaces the value of one secret field with what fn returns, or clears it
// when fn returns nothing.
func applySecret(
	m protoreflect.Message,
	fd protoreflect.FieldDescriptor,
	path string,
	fn func(path, value string) (string, error),
) error {
	if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
		return fmt.Errorf("%s is marked secret but is not a string field", path)
	}
	out, err := fn(path, m.Get(fd).String())
	if err != nil {
		return err
	}
	if out == "" {
		m.Clear(fd)
		return nil
	}
	m.Set(fd, protoreflect.ValueOfString(out))
	return nil
}

// isSecret says whether a field carries the mgmt.v1alpha1.secret annotation.
func isSecret(fd protoreflect.FieldDescriptor) bool {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	if !ok {
		return false
	}
	return proto.GetExtension(opts, mgmtv1alpha1.E_Secret).(bool)
}
