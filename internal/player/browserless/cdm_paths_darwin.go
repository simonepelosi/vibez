//go:build darwin

package browserless

import (
	"os"
	"path/filepath"
)

// Standard locations where libwidevinecdm.dylib may exist on macOS.
func candidatePaths() []string {
	home, _ := os.UserHomeDir()
	var res []string

	for _, pattern := range []string{
		filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "WidevineCdm", "*", "_platform_specific", "mac_arm64", "libwidevinecdm.dylib"),
		filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "WidevineCdm", "*", "_platform_specific", "mac_x64", "libwidevinecdm.dylib"),
		filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "WidevineCdm", "*", "_platform_specific", "mac_arm64", "libwidevinecdm.dylib"),
		filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "WidevineCdm", "*", "_platform_specific", "mac_x64", "libwidevinecdm.dylib"),
		filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*", "gmp-widevinecdm", "*", "libwidevinecdm.dylib"),
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			res = append(res, matches...)
		}
	}

	res = append(res,
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/Current/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/Current/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		"/Applications/Brave Browser.app/Contents/Frameworks/Brave Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Brave Browser.app/Contents/Frameworks/Brave Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		"/Applications/Brave Browser.app/Contents/Frameworks/Brave Framework.framework/Versions/Current/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Brave Browser.app/Contents/Frameworks/Brave Framework.framework/Versions/Current/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		"/Applications/Microsoft Edge.app/Contents/Frameworks/Microsoft Edge Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Microsoft Edge.app/Contents/Frameworks/Microsoft Edge Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		"/Applications/Arc.app/Contents/Frameworks/Arc Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_arm64/libwidevinecdm.dylib",
		"/Applications/Arc.app/Contents/Frameworks/Arc Framework.framework/Libraries/WidevineCdm/_platform_specific/mac_x64/libwidevinecdm.dylib",
		filepath.Join(home, "Library", "Caches", "vibez", "cdm", "libwidevinecdm.dylib"),
		filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.dylib"),
	)
	return res
}

// DefaultCDMPath returns the destination path for the cached standalone CDM library on macOS.
func DefaultCDMPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Caches", "vibez", "cdm", "libwidevinecdm.dylib")
}
