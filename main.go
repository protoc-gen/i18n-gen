package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/emicklei/proto"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func main() {
	// Define flags
	protoPattern := flag.String("P", "internal/common/xerr/errors.proto", "Path pattern to the .proto files (supports glob patterns)")
	outputDir := flag.String("O", "./i18n/", "Path to the output directory")
	languages := flag.String("L", "en,zh", "Comma-separated list of languages")
	enumPrefix := flag.String("prefix", "", "Only process enums with this prefix (optional)")
	enumSuffix := flag.String("suffix", "", "Only process enums with this suffix (optional)")
	flag.Parse()

	// Find all matching proto files recursively
	var protoFiles []string
	err := filepath.Walk(filepath.Dir(*protoPattern), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".proto") {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})
	if err != nil {
		log.Printf("Failed to find proto files: %v\n", err)
		return
	}
	if len(protoFiles) == 0 {
		log.Printf("No proto files found in directory: %s\n", filepath.Dir(*protoPattern))
		return
	}

	// Print found files for debugging
	// log.Printf("Found %d proto files:\n", len(protoFiles))
	// for _, file := range protoFiles {
	// 	log.Printf("- %s\n", file)
	// }

	// Parse all proto files and collect entries
	var allEntries []string
	allMessages := make(map[string]string)
	seenEntries := make(map[string]bool)
	for _, protoFile := range protoFiles {
		entries, messages, err := parseProto(protoFile, *enumPrefix, *enumSuffix)
		if err != nil {
			log.Printf("Failed to parse proto file %s: %v\n", protoFile, err)
			continue
		}

		// Add unique entries while maintaining order
		for _, entry := range entries {
			if !seenEntries[entry] {
				seenEntries[entry] = true
				allEntries = append(allEntries, entry)
				allMessages[entry] = messages[entry]
			}
		}
	}

	if len(allEntries) == 0 {
		log.Printf("No entries found in any proto files\n")
		return
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Printf("Failed to create output directory: %v\n", err)
		return
	}

	// Generate or update TOML files
	langList := strings.Split(*languages, ",")
	for _, lang := range langList {
		lang = strings.TrimSpace(lang)
		if lang == "" {
			continue
		}
		tomlPath := fmt.Sprintf("%s/%s.toml", *outputDir, lang)
		if err := generateTOML(allEntries, allMessages, tomlPath); err != nil {
			log.Printf("Failed to generate %s.toml: %v\n", lang, err)
			continue
		}
		log.Printf("%s.toml generated/updated successfully.", lang)
	}
}

// parseProto reads the .proto file and extracts enum names and validation IDs as keys in order.
func parseProto(filePath string, enumPrefix, enumSuffix string) ([]string, map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open proto file: %w", err)
	}
	defer file.Close()

	var entries []string
	reader := bufio.NewReader(file)
	parser := proto.NewParser(reader)

	definition, err := parser.Parse()
	if err != nil {
		return nil, nil, fmt.Errorf("parse proto: %w", err)
	}

	// First pass: collect enum entries
	proto.Walk(definition,
		proto.WithEnum(func(e *proto.Enum) {
			// Check if enum name matches prefix/suffix criteria
			if enumPrefix != "" && !strings.HasPrefix(e.Name, enumPrefix) {
				return
			}
			if enumSuffix != "" && !strings.HasSuffix(e.Name, enumSuffix) {
				return
			}

			for _, elem := range e.Elements {
				if field, ok := elem.(*proto.EnumField); ok {
					entries = append(entries, field.Name)
				}
			}
		}),
	)

	messages := make(map[string]string)
	// Second pass: read the file again to extract validation IDs
	file.Seek(0, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "(buf.validate.field).cel") {
			// Look for the next line containing "id:"
			var id string
			var message string
			for scanner.Scan() {
				nextLine := strings.TrimSpace(scanner.Text())
				// log.Printf("nextLine: %s", nextLine)
				if strings.HasPrefix(nextLine, "id:") {
					// Extract the ID value between quotes
					idStart := strings.Index(nextLine, "\"") + 1
					idEnd := strings.LastIndex(nextLine, "\"")
					if idStart > 0 && idEnd > idStart {
						id = nextLine[idStart:idEnd]
						entries = append(entries, id)
					}
					continue
				}
				if strings.HasPrefix(nextLine, "message:") {
					// Extract the message value between quotes
					msgStart := strings.Index(nextLine, "\"") + 1
					msgEnd := strings.LastIndex(nextLine, "\"")
					if msgStart > 0 && msgEnd > msgStart {
						message = nextLine[msgStart:msgEnd]
					}
					continue
				}
				if strings.HasPrefix(nextLine, "}];") || strings.HasPrefix(nextLine, "},") {
					if id != "" {
						// log.Printf("id: %s, message: %s", id, message)
						messages[id] = message
					}
					id, message = "", ""
					break
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read proto file: %w", err)
	}

	return entries, messages, nil
}

// generateTOML updates or creates a TOML file based on the provided entries and maintains order.
func generateTOML(entries []string, messages map[string]string, filePath string) error {
	existingEntries, err := loadExistingTOML(filePath)
	if err != nil {
		return fmt.Errorf("load existing TOML: %w", err)
	}

	// Merge existing entries while maintaining order
	entryMap := make(map[string]string)
	for _, entry := range entries {
		if val, exists := existingEntries[entry]; exists {
			entryMap[entry] = val
		} else {
			entryMap[entry] = ""
		}
	}

	// Generate TOML content
	var buffer bytes.Buffer
	for _, entry := range entries {
		existingMessage := entryMap[entry]
		if existingMessage == "" {
			existingMessage = messages[entry]
		}
		buffer.WriteString(fmt.Sprintf("[%s]\nother = \"%s\"\n\n", entry, existingMessage))
	}

	// Write the updated content to the file
	if err := os.WriteFile(filePath, buffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("write TOML file: %w", err)
	}

	return nil
}

// loadExistingTOML parses an existing TOML file into a map of keys with their values.
func loadExistingTOML(filePath string) (map[string]string, error) {
	entries := make(map[string]string)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return entries, nil // File does not exist, return empty map
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open TOML file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentKey string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentKey = line[1 : len(line)-1]
		} else if strings.HasPrefix(line, "other = ") {
			if currentKey != "" {
				entries[currentKey] = strings.Trim(line[len("other = "):], "\"")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read TOML file: %w", err)
	}

	return entries, nil
}

// snakeToCamelCase converts snake_case to CamelCase.
func snakeToCamelCase(input string) string {
	words := strings.Split(input, "_")
	caser := cases.Title(language.English) // Create a caser for proper title casing
	for i := range words {
		words[i] = caser.String(strings.ToLower(words[i]))
	}
	return strings.Join(words, "")
}
