package fonts

import (
	_ "embed"
	"fmt"

	fc "github.com/benoitkugler/textprocessing/fontconfig"
	"github.com/benoitkugler/textprocessing/pango/fcfonts"
	"github.com/benoitkugler/webrender/html/tree"
	resourcestest "github.com/benoitkugler/webrender/resources_test"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/utils"
)

// UAStylesheet is a lightweight style sheet
var UAStylesheet tree.CSS

const fontmapCache = "../../text/testdata/cache.fc"

// FontConfig is loaded with [UAStylesheet]
var FontConfig *text.FontConfigurationPango

func init() {
	// this command has to run once
	// fmt.Println("Scanning fonts...")
	// _, err := fc.ScanAndCache(fontmapCache)
	// if err != nil {
	// 	panic(err)
	// }
	fs, err := fc.LoadFontsetFile(fontmapCache)
	if err != nil {
		panic(err)
	}
	FontConfig = text.NewFontConfigurationPango(fcfonts.NewFontMap(fc.Standard.Copy(), fs))

	baseUrl, _ := utils.PathToURL("../../resources_test/")

	UAStylesheet, err = tree.NewCSSExt(utils.InputString(resourcestest.TestUACSS), baseUrl, FontConfig)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
}
