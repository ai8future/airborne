package service

import (
	"reflect"
	"strings"
	"testing"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestEmailAIProtoContract(t *testing.T) {
	request := (&pb.GenerateReplyRequest{}).ProtoReflect().Descriptor()
	tests := []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
	}{
		{name: "idempotency_key", number: 22},
		{name: "external_ref", number: 23},
	}
	for _, tt := range tests {
		field := request.Fields().ByName(tt.name)
		if field == nil || field.Number() != tt.number || field.Kind() != protoreflect.StringKind {
			t.Fatalf("GenerateReplyRequest.%s descriptor = %v, want string tag %d", tt.name, field, tt.number)
		}
	}

	goType := reflect.TypeOf(pb.GenerateReplyRequest{})
	for fieldName, wantTag := range map[string]string{"IdempotencyKey": "bytes,22", "ExternalRef": "bytes,23"} {
		field, ok := goType.FieldByName(fieldName)
		if !ok || !strings.Contains(field.Tag.Get("protobuf"), wantTag) {
			t.Fatalf("GenerateReplyRequest.%s protobuf tag = %q, want it to contain %q", fieldName, field.Tag.Get("protobuf"), wantTag)
		}
	}

	usage := (&pb.Usage{}).ProtoReflect().Descriptor()
	for _, name := range []protoreflect.Name{"input_tokens", "output_tokens", "total_tokens"} {
		field := usage.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.Int64Kind {
			t.Fatalf("Usage.%s kind = %v, want int64", name, field)
		}
	}
}
