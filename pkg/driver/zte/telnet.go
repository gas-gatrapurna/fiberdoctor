package zte

import (
	"context"
	"regexp"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver/cli"
)

// ztePromptRe mengenali baris prompt CLI OLT ZTE (mis. "OLT-SYNTH-A#").
var ztePromptRe = regexp.MustCompile(`(?m)^[A-Za-z][A-Za-z0-9_.\-]*[#>\]]\s*$`)

// TelnetClient mengelola sesi CLI Telnet murni Go ke OLT ZTE.
//
// Klien ini memakai mesin sesi generik `pkg/driver/cli`, yang sama dengan
// driver C-Data. mesin tersebut sudah tervalidasi terhadap perangkat live
// (SYNTH-A & SYNTH-B, 2026-11-05): pembacaan linear, framing prompt penuh,
// dan race-free.
type TelnetClient struct {
	Host           string
	Port           int
	Username       string
	Password       string
	TimeoutSeconds int
}

// client membuat instance klien CLI generik dengan konfigurasi OLT ZTE.
func (tc *TelnetClient) client() *cli.Client {
	return cli.New(cli.Options{
		Host:                 tc.Host,
		Port:                 tc.Port,
		Username:             tc.Username,
		Password:             tc.Password,
		TimeoutSeconds:       tc.TimeoutSeconds,
		UsernamePrompts:      []string{"username:", "user name:", "login:"},
		PasswordPrompts:      []string{"password:"},
		EnableCommand:        "enable",
		EnablePasswordPrompt: "password:",
		PromptRe:             ztePromptRe,
		DisablePagingCommand: "terminal length 0",
	})
}

// ExecuteCommand menjalankan satu perintah CLI dan mengembalikan output bersihnya.
func (tc *TelnetClient) ExecuteCommand(ctx context.Context, command string) (string, error) {
	return tc.client().Execute(ctx, command)
}

// ExecuteScript menjalankan rangkaian perintah pada satu sesi.
func (tc *TelnetClient) ExecuteScript(ctx context.Context, commands []string) (string, error) {
	return tc.client().ExecuteScript(ctx, commands)
}

// CleanCLIOutput membuang echo perintah, byte NUL, baris kosong di tepi, dan
// prompt di ekor output agar hasil siap diparsing.
func CleanCLIOutput(raw, command, prompt string) string {
	return cli.CleanOutput(raw, command, prompt)
}
