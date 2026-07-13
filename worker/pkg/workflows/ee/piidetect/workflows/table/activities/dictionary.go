package piidetect_table_activities

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"strings"
	"unicode"

	"go.temporal.io/sdk/activity"
)

//go:embed dictionaries/*.txt
var dictionaryFiles embed.FS

// piiColumnDictionary maps normalized column-name tokens (e.g. "date_naissance")
// to their PII category. Populated at init time from the embedded per-language files.
var piiColumnDictionary map[string]PiiCategory

func init() {
	dictionary, err := loadDictionaries(dictionaryFiles)
	if err != nil {
		panic(fmt.Sprintf("unable to load pii column dictionaries: %v", err))
	}
	piiColumnDictionary = dictionary
}

// loadDictionaries parses every embedded dictionary file.
// File format: one entry per line, "<token> <category>", "#" starts a comment.
func loadDictionaries(files embed.FS) (map[string]PiiCategory, error) {
	validCategories := map[string]PiiCategory{}
	for _, category := range GetAllPiiCategories() {
		validCategories[category.String()] = category
	}

	dictionary := map[string]PiiCategory{}
	entries, err := files.ReadDir("dictionaries")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		file, err := files.Open("dictionaries/" + entry.Name())
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 2 {
				file.Close()
				return nil, fmt.Errorf("%s:%d: expected '<token> <category>', got %q", entry.Name(), lineNo, line)
			}
			category, ok := validCategories[fields[1]]
			if !ok {
				file.Close()
				return nil, fmt.Errorf("%s:%d: unknown pii category %q", entry.Name(), lineNo, fields[1])
			}
			dictionary[strings.ToLower(fields[0])] = category
		}
		if err := scanner.Err(); err != nil {
			file.Close()
			return nil, err
		}
		file.Close()
	}
	return dictionary, nil
}

// DictionaryMatch describes a dictionary hit for a column.
type DictionaryMatch struct {
	Category PiiCategory `json:"category"`
	// Token is the normalized column-name token that matched the dictionary.
	Token string `json:"token"`
}

type DetectPiiDictionaryRequest struct {
	ColumnData []*ColumnData
}

type DetectPiiDictionaryResponse struct {
	PiiColumns map[string]DictionaryMatch
}

// DetectPiiDictionary matches normalized column names against the embedded
// multilingual dictionary of PII column-name tokens.
func (a *Activities) DetectPiiDictionary(
	ctx context.Context,
	req *DetectPiiDictionaryRequest,
) (*DetectPiiDictionaryResponse, error) {
	logger := activity.GetLogger(ctx)

	piiColumns := make(map[string]DictionaryMatch)
	for _, dbCol := range req.ColumnData {
		if match, ok := lookupColumnInDictionary(dbCol.Column); ok {
			piiColumns[dbCol.Column] = match
		}
	}

	logger.Debug("dictionary PII detection complete")

	return &DetectPiiDictionaryResponse{
		PiiColumns: piiColumns,
	}, nil
}

// lookupColumnInDictionary normalizes the column name into tokens and matches
// the full name, then bigrams, then single tokens against the dictionary.
func lookupColumnInDictionary(columnName string) (DictionaryMatch, bool) {
	tokens := normalizeColumnName(columnName)
	if len(tokens) == 0 {
		return DictionaryMatch{}, false
	}

	candidates := []string{strings.Join(tokens, "_")}
	for i := 0; i+1 < len(tokens); i++ {
		candidates = append(candidates, tokens[i]+"_"+tokens[i+1])
	}
	candidates = append(candidates, tokens...)

	for _, candidate := range candidates {
		if category, ok := piiColumnDictionary[candidate]; ok {
			return DictionaryMatch{Category: category, Token: candidate}, true
		}
	}
	return DictionaryMatch{}, false
}

// normalizeColumnName lowercases and splits a column name into tokens,
// handling snake_case, kebab-case, spaces, dots and camelCase boundaries.
func normalizeColumnName(columnName string) []string {
	var sb strings.Builder
	runes := []rune(columnName)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			sb.WriteRune('_')
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
				sb.WriteRune('_')
			}
			sb.WriteRune(unicode.ToLower(r))
		default:
			sb.WriteRune(unicode.ToLower(r))
		}
	}

	tokens := []string{}
	for _, token := range strings.Split(sb.String(), "_") {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}
