package affiliate

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/translation"
)

// The sentences of this package's screens. They are embedded and not
// published: an application that wants other words writes them in its own
// catalogue, under the same keys, and Config.Translator is asked first. One
// override path, the one the application already has.

//go:embed resources/lang
var langSources embed.FS

const (
	// TranslationGroup is the file every key of this package lives in, and the
	// prefix of every key: "affiliate.screen.dashboard_title".
	TranslationGroup = "affiliate"
	// FallbackLocale answers a request that negotiated no locale. The package
	// is Brazilian first.
	FallbackLocale = "pt-BR"
)

var shipped, shippedLocales = mustShipped()

func mustShipped() (*translation.Translator, []string) {
	root, err := fs.Sub(langSources, "resources/lang")
	if err != nil {
		panic("affiliate: the embedded catalogue has no lang directory: " + err.Error())
	}
	loader, err := translation.NewFileLoader(root, ".")
	if err != nil {
		panic("affiliate: the embedded catalogue does not parse: " + err.Error())
	}
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		panic("affiliate: the embedded catalogue cannot be listed: " + err.Error())
	}
	locales := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			locales = append(locales, entry.Name())
		}
	}
	sort.Strings(locales)
	if len(locales) == 0 {
		panic("affiliate: the embedded catalogue holds no locale, so every screen would draw its keys")
	}
	return translation.New(loader, FallbackLocale, FallbackLocale), locales
}

// Locales are the languages the package ships.
func Locales() []string { return append([]string(nil), shippedLocales...) }

// Labels are the sentences of this package's screens in one locale.
type Labels struct {
	Locale string
	module *Module
}

// T reads a key of this package: "screen.dashboard_title".
func (l Labels) T(key string) string {
	full := TranslationGroup + "." + key
	if l.module != nil && l.module.cfg.Translator != nil && l.module.cfg.Translator.Has(l.Locale, full) {
		return l.module.cfg.Translator.Get(l.Locale, full, nil)
	}
	return shipped.Get(l.Locale, full, nil)
}

// Status is the label of a status of this package: an affiliate's, a
// commission's or a conversion's.
func (l Labels) Status(s string) string { return l.T("status." + s) }

// Labels answers the sentences in a locale, the shipped one when empty.
func (m *Module) Labels(locale string) Labels {
	if strings.TrimSpace(locale) == "" {
		locale = FallbackLocale
	}
	return Labels{Locale: locale, module: m}
}
