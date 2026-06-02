package captcha

import (
	"fmt"
	"image"

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
	backgrounds, err := loadClickBackgrounds()
	if err != nil {
		return nil, fmt.Errorf("load click captcha backgrounds: %w", err)
	}
	return []click.Resource{
		click.WithChars(chars.GetAlphaChars()),
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
		click.WithRangeLen(option.RangeVal{Min: 6, Max: 6}),
		click.WithRangeVerifyLen(option.RangeVal{Min: 4, Max: 4}),
	)
	builder.SetResources(resources...)
	return builder.Make(), nil
}

func loadClickBackgrounds() ([]image.Image, error) {
	img, err := decodeEmbedImage("resources/click_images/background-1.png")
	if err != nil {
		return nil, err
	}
	return []image.Image{img}, nil
}
