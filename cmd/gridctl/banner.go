package main

import "fmt"

// Banner split into "grid" (amber) and "ctl" (white) parts.
// Based on Doom 2 font from patorjk.com/software/taag
var bannerGrid = []string{
	`            _     _`,
	`           (_)   | |`,
	`  __ _ _ __ _  __| |`,
	` / _` + "`" + ` | '__| |/ _` + "`" + ` |`,
	`| (_| | |  | | (_| |`,
	` \__, |_|  |_|\__,_|`,
	`  __/ |`,
	` |___/`,
}

var bannerCTL = []string{
	`      _   _`,
	`    | | | |`,
	` ___| |_| |`,
	`/ __| __| |`,
	` (__| |_| |`,
	`\___|\__|_|`,
	``,
	``,
}

// printBanner prints the ASCII logo with "grid" in amber and "ctl" in white.
func printBanner() {
	const (
		colorAmber = "\033[38;2;245;158;11m"
		colorWhite = "\033[97m"
		reset      = "\033[0m"
	)

	for i := 0; i < len(bannerGrid); i++ {
		fmt.Print(colorAmber + bannerGrid[i] + reset)
		if i < len(bannerCTL) && bannerCTL[i] != "" {
			fmt.Print(colorWhite + bannerCTL[i] + reset)
		}
		fmt.Println()
	}
	fmt.Println()
}
