package pg_models

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/testing/protocmp"
)

// Every option of every transformer has to survive being stored and read back. The stored
// struct copies the proto field by field, twice, and nothing ties the two: an option added
// to the proto and forgotten here is dropped on save, and the transformer runs without it,
// silently — the way the new column strategy first shipped (see new_column_strategy_test.go).
//
// So the cases are read off the proto: each transformer of the `config` oneof, with every
// one of its fields set to a value that is not the default.
func Test_TransformerConfig_RoundTrip(t *testing.T) {
	configs := (&mgmtv1alpha1.TransformerConfig{}).ProtoReflect().Descriptor().Oneofs().ByName("config")
	require.NotNil(t, configs)
	require.Positive(t, configs.Fields().Len())

	for i := 0; i < configs.Fields().Len(); i++ {
		field := configs.Fields().Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			dto := (&mgmtv1alpha1.TransformerConfig{}).ProtoReflect()
			variant := dto.NewField(field).Message()
			fillFields(variant, 0)
			dto.Set(field, protoreflect.ValueOfMessage(variant))

			stored := &TransformerConfig{}
			require.NoError(t, stored.FromTransformerConfigDto(dto.Interface().(*mgmtv1alpha1.TransformerConfig)))
			read := &TransformerConfig{}
			throughJson(t, stored, read)
			back, err := read.ToTransformerConfigDto()
			require.NoError(t, err)

			if diff := cmp.Diff(dto.Interface(), proto.Message(back), protocmp.Transform()); diff != "" {
				t.Errorf("%s loses options when stored (-sent +read back):\n%s", field.Name(), diff)
			}
		})
	}
}

// fillFields sets every field of m to a value that is not its default; for a oneof, its
// first field only.
func fillFields(m protoreflect.Message, depth int) {
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if oneof := f.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() && oneof.Fields().Get(0) != f {
			continue
		}
		switch {
		case f.IsMap():
			continue
		case f.IsList():
			list := m.Mutable(f).List()
			if f.Kind() == protoreflect.MessageKind {
				if depth < 3 {
					elem := list.NewElement()
					fillFields(elem.Message(), depth+1)
					list.Append(elem)
				}
				continue
			}
			list.Append(sampleScalar(f))
		case f.Kind() == protoreflect.MessageKind:
			if depth < 3 {
				fillFields(m.Mutable(f).Message(), depth+1)
			}
		default:
			m.Set(f, sampleScalar(f))
		}
	}
}

func sampleScalar(f protoreflect.FieldDescriptor) protoreflect.Value {
	switch f.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(true)
	case protoreflect.StringKind:
		return protoreflect.ValueOfString("sample")
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte("sample"))
	case protoreflect.EnumKind:
		values := f.Enum().Values()
		return protoreflect.ValueOfEnum(values.Get(values.Len() - 1).Number())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(7)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(7)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(7)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(7)
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(1.5)
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(1.5)
	}
	panic("unhandled kind " + f.Kind().String())
}
