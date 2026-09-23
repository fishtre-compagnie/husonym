package pg_models

import (
	"encoding/json"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Every strategy the API accepts has to survive being stored and read back. Each dialect keeps
// its own struct and its own two switches, and nothing ties them to the proto: a strategy added
// to the proto and forgotten here is dropped on save and read back as none — "continue", which
// ignores new columns — and nothing fails. That is how the first review strategy shipped:
// choosing it in the UI silently did nothing.
//
// So the cases are not listed by hand. They are read off the proto's oneof, and a new strategy
// fails this test until its persistence exists.
func Test_NewColumnAdditionStrategy_RoundTrip(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		eachStrategy(t, &mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy{},
			func(dto proto.Message) proto.Message {
				stored := &PostgresNewColumnAdditionStrategy{}
				stored.FromDto(dto.(*mgmtv1alpha1.PostgresSourceConnectionOptions_NewColumnAdditionStrategy))
				read := &PostgresNewColumnAdditionStrategy{}
				throughJson(t, stored, read)
				return read.ToDto()
			})
	})
	t.Run("mysql", func(t *testing.T) {
		eachStrategy(t, &mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy{},
			func(dto proto.Message) proto.Message {
				stored := &MysqlNewColumnAdditionStrategy{}
				stored.FromDto(dto.(*mgmtv1alpha1.MysqlSourceConnectionOptions_NewColumnAdditionStrategy))
				read := &MysqlNewColumnAdditionStrategy{}
				throughJson(t, stored, read)
				return read.ToDto()
			})
	})
	t.Run("mssql", func(t *testing.T) {
		eachStrategy(t, &mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy{},
			func(dto proto.Message) proto.Message {
				stored := &MssqlNewColumnAdditionStrategy{}
				stored.FromDto(dto.(*mgmtv1alpha1.MssqlSourceConnectionOptions_NewColumnAdditionStrategy))
				read := &MssqlNewColumnAdditionStrategy{}
				throughJson(t, stored, read)
				return read.ToDto()
			})
	})
}

// eachStrategy sets, one at a time, every field of the message's `strategy` oneof, and checks
// that roundTrip hands back an equal message.
func eachStrategy(t *testing.T, empty proto.Message, roundTrip func(proto.Message) proto.Message) {
	t.Helper()
	strategies := empty.ProtoReflect().Descriptor().Oneofs().ByName("strategy")
	require.NotNil(t, strategies, "no strategy oneof on %s", empty.ProtoReflect().Descriptor().FullName())
	require.Positive(t, strategies.Fields().Len())

	for i := 0; i < strategies.Fields().Len(); i++ {
		field := strategies.Fields().Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			dto := empty.ProtoReflect().New()
			dto.Set(field, protoreflect.ValueOfMessage(dto.NewField(field).Message()))
			require.True(t,
				proto.Equal(dto.Interface(), roundTrip(dto.Interface())),
				"%s does not survive being stored: it is read back as another strategy, or as none",
				field.Name(),
			)
		})
	}
}

// throughJson stores the way the jobs table does, as JSON, and reads it back.
func throughJson(t *testing.T, stored, read any) {
	t.Helper()
	bits, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(bits, read))
}
