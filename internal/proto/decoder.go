package proto

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bufbuild/protocompile"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Decoder handles dynamic protobuf message decoding
type Decoder struct {
	messageTypes map[string]protoreflect.MessageDescriptor
	allMessages  []protoreflect.MessageDescriptor
	ParseErrors  []string
}

// NewDecoder creates a decoder from a directory of .proto files
func NewDecoder(protoPath string) (*Decoder, error) {
	var parseErrors []string

	// Find all .proto files
	var protoFiles []string
	err := filepath.Walk(protoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".proto") {
			relPath, err := filepath.Rel(protoPath, path)
			if err != nil {
				relPath = path
			}
			protoFiles = append(protoFiles, relPath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk proto path: %w", err)
	}

	if len(protoFiles) == 0 {
		return nil, fmt.Errorf("no .proto files found in %s", protoPath)
	}

	parseErrors = append(parseErrors, fmt.Sprintf("Found %d proto files", len(protoFiles)))

	// Parse proto files with well-known type support
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(
			&protocompile.SourceResolver{
				ImportPaths: []string{protoPath},
			},
		),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	// Try to compile all files, collecting successful ones
	var fds []protoreflect.FileDescriptor
	for _, pf := range protoFiles {
		compiled, err := compiler.Compile(context.Background(), pf)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", pf, err))
			continue
		}
		for _, fd := range compiled {
			fds = append(fds, fd)
		}
	}

	// Build message type map
	messageTypes := make(map[string]protoreflect.MessageDescriptor)
	var allMessages []protoreflect.MessageDescriptor

	for _, fd := range fds {
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			md := msgs.Get(i)
			messageTypes[string(md.Name())] = md
			messageTypes[string(md.FullName())] = md
			allMessages = append(allMessages, md)
		}
	}

	return &Decoder{
		messageTypes: messageTypes,
		allMessages:  allMessages,
		ParseErrors:  parseErrors,
	}, nil
}

// Decode attempts to decode protobuf bytes using known message types
// Returns decoded fields as a map
func (d *Decoder) Decode(data []byte) (map[string]any, error) {
	return d.DecodeWithHint(data, "")
}

// DecodeWithHint decodes using a routing key hint to pick the right message type
func (d *Decoder) DecodeWithHint(data []byte, routingKey string) (map[string]any, error) {
	result, _, err := d.DecodeWithHintAndType(data, routingKey)
	return result, err
}

// DecodeWithHintAndType decodes and returns both the result and the detected type name
func (d *Decoder) DecodeWithHintAndType(data []byte, routingKey string) (map[string]any, string, error) {
	if d == nil || len(d.allMessages) == 0 {
		return nil, "", fmt.Errorf("no message types loaded")
	}

	// Extract hint from routing key (e.g., "editorial.it.country.updated" -> "CountryUpdated")
	typeHint := routingKeyToTypeHint(routingKey)

	// Try each message type and find the best match
	var bestMatch *dynamicpb.Message
	var bestMatchName string
	bestScore := 0

	for _, md := range d.allMessages {
		msg := dynamicpb.NewMessage(md)
		if err := proto.Unmarshal(data, msg); err != nil {
			continue
		}

		// Score based on how many fields were populated
		score := countPopulatedFields(msg)

		// Boost score if name matches the routing key hint
		name := string(md.Name())
		if typeHint != "" && strings.EqualFold(name, typeHint) {
			score += 1000 // Strong preference for matching type
		}

		if score > bestScore {
			bestScore = score
			bestMatch = msg
			bestMatchName = name
		}
	}

	if bestMatch == nil {
		return nil, "", fmt.Errorf("could not decode with any known message type")
	}

	result := messageToMap(bestMatch)
	result["__type"] = bestMatchName
	return result, bestMatchName, nil
}

// routingKeyToTypeHint converts routing key to expected message type
// e.g., "editorial.it.country.updated" -> "CountryUpdated"
func routingKeyToTypeHint(routingKey string) string {
	parts := strings.Split(routingKey, ".")
	if len(parts) < 2 {
		return ""
	}

	// Get last two parts: entity and action (e.g., "country" + "updated")
	entity := parts[len(parts)-2]
	action := parts[len(parts)-1]

	// Convert to PascalCase (e.g., "CountryUpdated")
	titleCaser := cases.Title(language.English)
	entity = titleCaser.String(strings.ToLower(entity))
	action = titleCaser.String(strings.ToLower(action))

	// Handle snake_case entities (e.g., "administrative_area" -> "AdministrativeArea")
	entity = strings.ReplaceAll(entity, "_", " ")
	entity = titleCaser.String(entity)
	entity = strings.ReplaceAll(entity, " ", "")

	return entity + action
}

// DecodeAs decodes using a specific message type name
func (d *Decoder) DecodeAs(data []byte, typeName string) (map[string]any, error) {
	md, ok := d.messageTypes[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown message type: %s", typeName)
	}

	msg := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	result := messageToMap(msg)
	result["__type"] = typeName
	return result, nil
}

// ListTypes returns all known message type names
func (d *Decoder) ListTypes() []string {
	var types []string
	for name := range d.messageTypes {
		types = append(types, name)
	}
	return types
}

func countPopulatedFields(msg *dynamicpb.Message) int {
	count := 0
	msg.Range(func(_ protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		count++
		return true
	})
	return count
}

func messageToMap(msg *dynamicpb.Message) map[string]any {
	result := make(map[string]any)
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		result[string(fd.Name())] = convertValue(fd, v)
		return true
	})
	return result
}

func convertValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	// Handle repeated fields (lists)
	if fd.IsList() {
		list := v.List()
		result := make([]any, list.Len())
		for i := 0; i < list.Len(); i++ {
			result[i] = convertSingularValue(fd, list.Get(i))
		}
		return result
	}

	// Handle map fields
	if fd.IsMap() {
		result := make(map[string]any)
		valDesc := fd.MapValue()
		v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
			key := fmt.Sprintf("%v", k.Value().Interface())
			result[key] = convertSingularValue(valDesc, mv)
			return true
		})
		return result
	}

	return convertSingularValue(fd, v)
}

func convertSingularValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := v.Message()
		result := make(map[string]any)
		nested.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			result[string(fd.Name())] = convertValue(fd, v)
			return true
		})
		return result
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(v.Bytes())
	case protoreflect.EnumKind:
		return int32(v.Enum())
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return v.Int()
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return v.Uint()
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return v.Uint()
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return v.Float()
	default:
		return v.Interface()
	}
}
