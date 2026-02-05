package proto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"
)

// Decoder handles dynamic protobuf message decoding
type Decoder struct {
	messageTypes map[string]*desc.MessageDescriptor
	allMessages  []*desc.MessageDescriptor
}

// ParseErrors holds errors encountered during parsing
var ParseErrors []string

// NewDecoder creates a decoder from a directory of .proto files
func NewDecoder(protoPath string) (*Decoder, error) {
	ParseErrors = nil // Reset errors

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

	ParseErrors = append(ParseErrors, fmt.Sprintf("Found %d proto files", len(protoFiles)))

	// Parse proto files with well-known type support
	parser := protoparse.Parser{
		ImportPaths:           []string{protoPath},
		IncludeSourceCodeInfo: true,
	}

	// Try to parse all files, collecting successful ones
	var fds []*desc.FileDescriptor
	for _, pf := range protoFiles {
		fd, err := parser.ParseFiles(pf)
		if err != nil {
			ParseErrors = append(ParseErrors, fmt.Sprintf("%s: %v", pf, err))
			continue
		}
		fds = append(fds, fd...)
	}

	// Build message type map
	messageTypes := make(map[string]*desc.MessageDescriptor)
	var allMessages []*desc.MessageDescriptor

	for _, fd := range fds {
		for _, md := range fd.GetMessageTypes() {
			messageTypes[md.GetName()] = md
			messageTypes[md.GetFullyQualifiedName()] = md
			allMessages = append(allMessages, md)
		}
	}

	return &Decoder{
		messageTypes: messageTypes,
		allMessages:  allMessages,
	}, nil
}

// Decode attempts to decode protobuf bytes using known message types
// Returns decoded fields as a map
func (d *Decoder) Decode(data []byte) (map[string]any, error) {
	return d.DecodeWithHint(data, "")
}

// DecodeWithHint decodes using a routing key hint to pick the right message type
func (d *Decoder) DecodeWithHint(data []byte, routingKey string) (map[string]any, error) {
	if d == nil || len(d.allMessages) == 0 {
		return nil, fmt.Errorf("no message types loaded")
	}

	// Extract hint from routing key (e.g., "editorial.it.country.updated" -> "CountryUpdated")
	typeHint := routingKeyToTypeHint(routingKey)

	// Try each message type and find the best match
	var bestMatch *dynamic.Message
	var bestMatchName string
	bestScore := 0

	for _, md := range d.allMessages {
		msg := dynamic.NewMessage(md)
		err := msg.Unmarshal(data)
		if err != nil {
			continue
		}

		// Score based on how many fields were populated
		score := countPopulatedFields(msg)

		// Boost score if name matches the routing key hint
		name := md.GetName()
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
		return nil, fmt.Errorf("could not decode with any known message type")
	}

	result := messageToMap(bestMatch)
	result["__type"] = bestMatchName
	return result, nil
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
	entity = strings.Title(strings.ToLower(entity))
	action = strings.Title(strings.ToLower(action))

	// Handle snake_case entities (e.g., "administrative_area" -> "AdministrativeArea")
	entity = strings.ReplaceAll(entity, "_", " ")
	entity = strings.Title(entity)
	entity = strings.ReplaceAll(entity, " ", "")

	return entity + action
}

// DecodeAs decodes using a specific message type name
func (d *Decoder) DecodeAs(data []byte, typeName string) (map[string]any, error) {
	md, ok := d.messageTypes[typeName]
	if !ok {
		return nil, fmt.Errorf("unknown message type: %s", typeName)
	}

	msg := dynamic.NewMessage(md)
	if err := msg.Unmarshal(data); err != nil {
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

func countPopulatedFields(msg *dynamic.Message) int {
	count := 0
	for _, fd := range msg.GetKnownFields() {
		if msg.HasField(fd) {
			count++
		}
	}
	return count
}

func messageToMap(msg *dynamic.Message) map[string]any {
	result := make(map[string]any)

	for _, fd := range msg.GetKnownFields() {
		if !msg.HasField(fd) {
			continue
		}

		val := msg.GetField(fd)
		result[fd.GetName()] = convertValue(val)
	}

	return result
}

func convertValue(val any) any {
	switch v := val.(type) {
	case *dynamic.Message:
		return messageToMap(v)
	case []byte:
		// Try to decode as string, otherwise return hex
		if isValidUTF8(v) {
			return string(v)
		}
		return fmt.Sprintf("0x%x", v)
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = convertValue(item)
		}
		return result
	default:
		return v
	}
}

func isValidUTF8(data []byte) bool {
	for _, b := range data {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}
