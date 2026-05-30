package captcha

import (
	"fmt"

	"github.com/golang/freetype/truetype"
	"github.com/wenlng/go-captcha-assets/bindata/chars"
	"github.com/wenlng/go-captcha-assets/resources/fonts/fzshengsksjw"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
)

func loadClickResources() ([]click.Resource, error) {
	font, err := fzshengsksjw.GetFont()
	if err != nil {
		return nil, fmt.Errorf("load click captcha font: %w", err)
	}
	backgrounds, err := loadBackgrounds()
	if err != nil {
		return nil, fmt.Errorf("load click captcha backgrounds: %w", err)
	}
	return []click.Resource{
		click.WithChars(chars.GetChineseChars()),
		click.WithFonts([]*truetype.Font{font}),
		click.WithBackgrounds(backgrounds),
	}, nil
}

func newClickCaptcha() (click.Captcha, error) {
	resources, err := loadClickResources()
	if err != nil {
		return nil, err
	}
	builder := click.NewBuilder(
		click.WithRangeLen(option.RangeVal{Min: 4, Max: 6}),
		click.WithRangeVerifyLen(option.RangeVal{Min: 2, Max: 4}),
	)
	builder.SetResources(resources...)
	return builder.Make(), nil
}
