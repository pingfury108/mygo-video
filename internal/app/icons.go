package app

import "github.com/egoist/mygo/ui"

// Icons are drawn on a 24-point grid, in the color of the text: transport
// controls are filled, everything else is a stroke.

func strokeIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func fillIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + paths + `</svg>`))
}

var (
	iconHome     = strokeIcon(`<path d="M12 3 4 9v11a1 1 0 0 0 1 1h4v-6h6v6h4a1 1 0 0 0 1-1V9z"/>`)
	iconLibrary  = strokeIcon(`<rect x="3" y="4" width="8" height="16" rx="1.4"/><rect x="13" y="4" width="8" height="16" rx="1.4"/>`)
	iconFilm     = strokeIcon(`<rect x="2.5" y="4.5" width="19" height="15" rx="2"/><path d="M7 4.5v15M17 4.5v15M2.5 9.5h4.5M2.5 14.5h4.5M17 9.5h4.5M17 14.5h4.5"/>`)
	iconSearch   = strokeIcon(`<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`)
	iconSettings = strokeIcon(`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>`)
	iconPlay     = fillIcon(`<path d="M7 4.6v14.8c0 .8.9 1.3 1.6.9l12-7.4c.6-.4.6-1.3 0-1.7l-12-7.4C7.9 3.3 7 3.8 7 4.6z"/>`)
	iconPause    = fillIcon(`<rect x="5.5" y="4" width="4.5" height="16" rx="1.4"/><rect x="14" y="4" width="4.5" height="16" rx="1.4"/>`)
	iconBack     = strokeIcon(`<path d="m15 18-6-6 6-6"/>`)
	iconForward  = strokeIcon(`<path d="m9 18 6-6-6-6"/>`)
	iconFolder   = strokeIcon(`<path d="M3 7a2 2 0 0 1 2-2h4l2 2.5h8a2 2 0 0 1 2 2V18a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`)
	iconVolume   = strokeIcon(`<path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z"/><path d="M16 9a5 5 0 0 1 0 6"/><path d="M19.4 18.4a9 9 0 0 0 0-12.8"/>`)
	iconMute     = strokeIcon(`<path d="M11 4.7a.7.7 0 0 0-1.2-.5L6.4 7.6A1.4 1.4 0 0 1 5.4 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.4a1.4 1.4 0 0 1 1 .4l3.4 3.4a.7.7 0 0 0 1.2-.5z"/><path d="m17 9 5 5M22 9l-5 5"/>`)
	iconRefresh  = strokeIcon(`<path d="M21 12a9 9 0 1 1-2.6-6.4"/><path d="M21 3v6h-6"/>`)
	iconClock    = strokeIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/>`)
	iconMore     = strokeIcon(`<circle cx="5" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="19" cy="12" r="1.4"/>`)
	iconClose    = strokeIcon(`<path d="M6 6l12 12M18 6L6 18"/>`)
	iconSubs     = strokeIcon(`<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 13h4M13 13h4M7 16h3"/>`)
	iconFull     = strokeIcon(`<path d="M4 9V4h5M20 15v5h-5M15 4h5v5M9 20H4v-5"/>`)
	iconEpisodes = strokeIcon(`<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 9h18M9 5v14"/>`)
	iconPrev     = fillIcon(`<path d="M6 5h2v14H6zM19 5.5v13L9 12z"/>`)
	iconNext     = fillIcon(`<path d="M16 5h2v14h-2zM5 5.5v13L15 12z"/>`)
	iconCheck    = strokeIcon(`<path d="m5 12 4 4L19 6"/>`)
	iconDown     = strokeIcon(`<path d="m6 9 6 6 6-6"/>`)
	iconArchive  = strokeIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v5M9 7h6M9 11h6M12 13v4"/>`)
	iconGlobe    = strokeIcon(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18a15 15 0 0 1 0-18"/>`)
)
