package prose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type runnerFunc func(context.Context, string, []string, string) ([]byte, int, error)

func (f runnerFunc) Run(ctx context.Context, bin string, args []string, input string) ([]byte, int, error) {
	return f(ctx, bin, args, input)
}

func TestNormalization(t *testing.T) {
	input := "🙂\n🙂これは冗長です。"
	issues, err := parseTextlint([]byte(`[{"messages":[{"ruleId":"redundant","message":"冗長です","range":[8,10]}]}]`), input, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Start != 6 || issues[0].End != 8 || issues[0].Line != 2 || issues[0].Column != 5 || issues[0].Text != "冗長" || issues[0].Severity != "warning" {
		t.Fatalf("issues = %+v", issues)
	}
	found, err := parseNaturalJapanese([]byte(`{"findings":[{"line":2,"category":"translationese","excerpt":"冗長","detail":"翻訳調"},{"line":1,"category":"rhythm","excerpt":"文数=10","detail":"単調"}]}`), input, input)
	if err != nil {
		t.Fatal(err)
	}
	if found[0].Start != 6 || found[0].Text != "冗長" || found[1].Start != 0 || found[1].End != 0 || found[1].Details["position_precision"] != "line" {
		t.Fatalf("issues = %+v", found)
	}
}

func TestMalformedProtocols(t *testing.T) {
	for _, data := range []string{"{", "null", "{}", "[{}]", `[{"messages":null}]`, `[{"messages":[{}]}]`, `[{"messages":[{"ruleId":"r","message":"m","range":[1,2]}]}]`, `[{"messages":[{"ruleId":"r","message":"m","range":[3,2]}]}]`} {
		if _, err := parseTextlint([]byte(data), "🙂日", "🙂日"); err == nil {
			t.Errorf("accepted textlint %s", data)
		}
	}
	for _, data := range []string{"{", "null", "{}", `{"findings":null}`, `{"findings":[{}]}`, `{"findings":[{"category":"r","detail":"m","line":2}]}`} {
		if _, err := parseNaturalJapanese([]byte(data), "日", "日"); err == nil {
			t.Errorf("accepted natural-japanese %s", data)
		}
	}
}

func TestDisabledAndFailures(t *testing.T) {
	config := configFile(t)
	runner := runnerFunc(func(context.Context, string, []string, string) ([]byte, int, error) {
		t.Fatal("disabled tool invoked")
		return nil, 0, nil
	})
	if _, err := Check(context.Background(), "日本語", Options{TextlintBin: "/missing", NaturalJapaneseScript: "/missing"}, runner); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), "", Options{Textlint: true, TextlintConfig: "/missing"}, runner); err == nil {
		t.Fatal("missing config accepted")
	}
	if _, err := Check(context.Background(), "", Options{NaturalJapanese: true}, runner); err == nil {
		t.Fatal("missing script accepted")
	}
	if _, err := Check(context.Background(), "", Options{NaturalJapanese: true, NaturalJapaneseGenre: "invalid"}, runner); err == nil {
		t.Fatal("invalid genre accepted")
	}
	if _, err := Check(context.Background(), "", Options{Textlint: true, TextlintConfig: config, TextlintBin: filepath.Join(t.TempDir(), "missing")}, ExecRunner{}); err == nil {
		t.Fatal("missing executable accepted")
	}
	for _, code := range []int{0, 1, 2, -1} {
		runner := runnerFunc(func(context.Context, string, []string, string) ([]byte, int, error) {
			return []byte(`[{"messages":[]}]`), code, nil
		})
		_, err := Check(context.Background(), "", Options{Textlint: true, TextlintConfig: config}, runner)
		if (err == nil) != (code == 0 || code == 1) {
			t.Errorf("exit %d: %v", code, err)
		}
	}
}

func TestTextlintRejectsMalformedOutputRegardlessOfExitCode(t *testing.T) {
	config := configFile(t)
	for _, code := range []int{0, 1} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			runner := runnerFunc(func(context.Context, string, []string, string) ([]byte, int, error) {
				var err error
				if code != 0 {
					err = fmt.Errorf("exit status %d", code)
				}
				return []byte("invalid-json"), code, err
			})
			_, err := Check(context.Background(), "日本語", Options{Textlint: true, TextlintConfig: config}, runner)
			if err == nil || !strings.Contains(err.Error(), "textlint:") {
				t.Fatalf("expected textlint parse error, got %v", err)
			}
		})
	}
}

func TestCommandsMaskingAndCleanup(t *testing.T) {
	config := configFile(t)
	input := "🙂 `禁止語` https://example.com/🙂\n```go\n禁止語\n```\nこれは日本語です。"
	script := filepath.Join(t.TempDir(), "lint.py")
	if err := os.WriteFile(script, []byte("# fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var temp string
	runner := runnerFunc(func(ctx context.Context, bin string, args []string, stdin string) ([]byte, int, error) {
		calls++
		if calls == 1 {
			if bin != "custom-textlint" || !reflect.DeepEqual(args, []string{"--stdin", "--stdin-filename", "response.md", "--format", "json", "--config", config}) {
				t.Fatalf("command %s %v", bin, args)
			}
			if !strings.Contains(stdin, "```go") || strings.Contains(stdin, "https://") {
				t.Fatalf("textlint input %q", stdin)
			}
			return []byte(`[{"messages":[]}]`), 0, nil
		}
		if bin != "custom-uv" || !reflect.DeepEqual(args[:6], []string{"run", "--offline", "--no-python-downloads", "--no-project", "--script", script}) {
			t.Fatalf("command %s %v", bin, args)
		}
		temp = args[6]
		if !reflect.DeepEqual(args[7:], []string{"--json", "--genre", "tech"}) {
			t.Fatal(args)
		}
		data, err := os.ReadFile(temp)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "禁止語") || strings.Contains(string(data), "https://") || len([]rune(string(data))) != len([]rune(input)) {
			t.Fatalf("masked %q", data)
		}
		return []byte(`{"findings":[{"line":5,"category":"translationese","excerpt":"これは日本語です。","detail":"翻訳調"}]}`), 0, nil
	})
	opts := Options{Textlint: true, TextlintConfig: config, TextlintBin: "custom-textlint", NaturalJapanese: true, NaturalJapaneseScript: script, NaturalJapaneseGenre: "tech", UVBin: "custom-uv"}
	issues, err := Check(context.Background(), input, opts, runner)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(issues) != 1 || issues[0].Line != 5 {
		t.Fatalf("calls=%d issues=%+v", calls, issues)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temp file remains: %v", err)
	}
	for _, code := range []int{1, 2} {
		_, err := Check(context.Background(), input, Options{NaturalJapanese: true, NaturalJapaneseScript: script}, runnerFunc(func(context.Context, string, []string, string) ([]byte, int, error) {
			return []byte(`{"findings":[]}`), code, nil
		}))
		if err == nil {
			t.Fatalf("natural-japanese exit %d accepted", code)
		}
	}
}

func TestTextlintMaskedPositionsAndCodeFilter(t *testing.T) {
	input := "`🙂` 冗長"
	issues, err := parseTextlint([]byte(`[{"messages":[{"ruleId":"r","message":"code","range":[1,3]},{"ruleId":"r","message":"prose","index":5}]}]`), input, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Start != 4 || issues[0].Text != "冗" {
		t.Fatalf("%+v", issues)
	}
}

func TestExecRunner(t *testing.T) {
	// The Go test executable itself is a portable subprocess fixture.
	t.Setenv("JPQG_PROCESS_FIXTURE", "1")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, code, err := (ExecRunner{}).Run(context.Background(), bin, []string{"-test.run=^TestProcessFixture$"}, "")
	if err == nil || code != 1 || string(data) != "[]" {
		t.Fatalf("%q %d %v", data, code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (ExecRunner{}).Run(ctx, bin, nil, ""); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestProcessFixture(t *testing.T) {
	if os.Getenv("JPQG_PROCESS_FIXTURE") != "1" {
		return
	}
	fmt.Print("[]")
	os.Exit(1)
}

func BenchmarkOptionalTextlint(b *testing.B) {
	config := configFile(b)
	runner := runnerFunc(func(context.Context, string, []string, string) ([]byte, int, error) {
		return []byte(`[{"messages":[]}]`), 0, nil
	})
	for i := 0; i < b.N; i++ {
		if _, err := Check(context.Background(), "これは日本語です。", Options{Textlint: true, TextlintConfig: config}, runner); err != nil {
			b.Fatal(err)
		}
	}
}

// Opt-in contract checks use tools installed by the developer, never downloads.
func TestInstalledLinters(t *testing.T) {
	for _, source := range []string{"textlint", "natural-japanese"} {
		t.Run(source, func(t *testing.T) {
			var options Options
			if source == "textlint" {
				if os.Getenv("JPQG_TEST_TEXTLINT_BIN") == "" {
					t.Skip("set JPQG_TEST_TEXTLINT_BIN and JPQG_TEST_TEXTLINT_CONFIG")
				}
				options = Options{Textlint: true, TextlintBin: os.Getenv("JPQG_TEST_TEXTLINT_BIN"), TextlintConfig: os.Getenv("JPQG_TEST_TEXTLINT_CONFIG")}
			} else {
				if os.Getenv("JPQG_TEST_NATURAL_JAPANESE_SCRIPT") == "" {
					t.Skip("set JPQG_TEST_NATURAL_JAPANESE_SCRIPT")
				}
				options = Options{NaturalJapanese: true, NaturalJapaneseScript: os.Getenv("JPQG_TEST_NATURAL_JAPANESE_SCRIPT"), UVBin: os.Getenv("JPQG_TEST_UV_BIN")}
			}
			for _, tc := range []struct {
				name, input string
				findings    bool
			}{
				{"valid", "使い方を説明します。", false},
				{"ai", "これは包括的で革命的な説明です。", true},
				{"translation", "この機能を使用することができます。", true},
				{"code", "```\nこれは包括的で革命的な説明です。\n```\n`これは包括的で革命的な説明です。`\nhttps://example.com/包括的", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					start := time.Now()
					issues, err := Check(context.Background(), tc.input, options, ExecRunner{})
					t.Logf("%s latency: %s", source, time.Since(start))
					if err != nil {
						t.Fatal(err)
					}
					if (len(issues) > 0) != tc.findings {
						t.Fatalf("issues = %+v", issues)
					}
					for _, issue := range issues {
						if issue.Severity != "warning" {
							t.Fatalf("severity = %s", issue.Severity)
						}
					}
				})
			}
		})
	}
}

func TestNaturalJapanesePreservesPositionsAcrossLineSeparators(t *testing.T) {
	script := filepath.Join(t.TempDir(), "lint.py")
	if err := os.WriteFile(script, nil, 0600); err != nil {
		t.Fatal(err)
	}
	input := "🙂\r\n前\r中\u2028後\n包括的"
	runner := runnerFunc(func(_ context.Context, _ string, args []string, _ string) ([]byte, int, error) {
		data, err := os.ReadFile(args[6])
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "🙂 \n前 中 後\n包括的" {
			t.Fatalf("input = %q", data)
		}
		return []byte(`{"findings":[{"line":3,"category":"forbidden_phrase","excerpt":"包括的","detail":"表現を修正"}]}`), 0, nil
	})
	issues, err := Check(context.Background(), input, Options{NaturalJapanese: true, NaturalJapaneseScript: script}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Start != 9 || issues[0].Line != 3 || issues[0].Column != 1 || issues[0].Text != "包括的" {
		t.Fatalf("issues = %+v", issues)
	}
}

func configFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".textlintrc.json")
	if err := os.WriteFile(path, []byte(`{"rules":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
