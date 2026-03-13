package fonts

import (
	_ "embed"
	"fmt"
	"log"

	fc "github.com/benoitkugler/textprocessing/fontconfig"
	"github.com/benoitkugler/textprocessing/pango/fcfonts"
	"github.com/benoitkugler/webrender/html/tree"
	resourcestest "github.com/benoitkugler/webrender/resources_test"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/utils"
	"github.com/go-text/typesetting/fontscan"
)

// UAStylesheet is a lightweight style sheet
var UAStylesheet tree.CSS

const (
	fontmapCachePango  = "../../text/testdata/cache.fc"
	fontmapCacheGotext = "../../text/testdata"

	useGoText = false
)

// FontConfig is loaded with [UAStylesheet]
var FontConfig text.FontConfiguration

func init() {
	var err error

	if useGoText {
		fontmapGotext := fontscan.NewFontMap(log.Default())
		err := fontmapGotext.UseSystemFonts(fontmapCacheGotext)
		if err != nil {
			panic(err)
		}
		FontConfig = text.NewFontConfigurationGotext(fontmapGotext)
	} else {
		// // this command has to run once
		// fmt.Println("Scanning fonts...")
		// _, err = fc.ScanAndCache(fontmapCachePango)
		// if err != nil {
		// 	panic(err)
		// }
		fs, err := fc.LoadFontsetFile(fontmapCachePango)
		if err != nil {
			panic(err)
		}
		FontConfig = text.NewFontConfigurationPango(fcfonts.NewFontMap(fc.Standard.Copy(), fs))
	}

	baseUrl, _ := utils.PathToURL("../../resources_test/")

	UAStylesheet, err = tree.NewCSSExt(utils.InputString(resourcestest.TestUACSS), baseUrl, FontConfig)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded stylesheet: %s", err))
	}
}
