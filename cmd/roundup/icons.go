package main

import "github.com/egoist/mygo/ui"

type parsedIcon struct {
	svg      *ui.SVG
	template bool
}

// icon is the parsed icon a source registered under key, or nil when it has none.
func (a *app) icon(source, key string) *parsedIcon {
	id := [2]string{source, key}
	if icon, ok := a.icons[id]; ok {
		return icon
	}
	var icon *parsedIcon
	if src, ok := a.st.sources[source]; ok {
		if def, ok := src.info.Icons[key]; ok {
			if svg, err := ui.ParseSVG(def.SVG); err == nil {
				icon = &parsedIcon{svg: svg, template: def.Template}
			}
		}
	}
	if a.icons == nil {
		a.icons = map[[2]string]*parsedIcon{}
	}
	a.icons[id] = icon
	return icon
}

// drawIcon draws a source's icon square: tinted with the text colour when it is a template, in its own colours otherwise.
func (a *app) drawIcon(c *ui.Context, source, key string, size float32) ui.Element {
	icon := a.icon(source, key)
	switch {
	case icon == nil:
		return ui.Row(c).Size(size, size)
	case icon.template:
		return ui.Icon(c, icon.svg).Size(size, size)
	}
	return ui.Image(c, icon.svg).Size(size, size)
}

// sourceIcon draws the source's own mark.
func (a *app) sourceIcon(c *ui.Context, source string, size float32) {
	var key string
	if src, ok := a.st.sources[source]; ok {
		key = src.info.Icon
	}
	a.drawIcon(c, source, key, size)
}
