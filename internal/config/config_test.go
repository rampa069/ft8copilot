package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadSample(t *testing.T) {
	cfg, err := Load("../../ft8ctrl.yaml.sample")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.FT8Ctrl.WSJTPort != 2238 {
		t.Errorf("WSJTPort = %d, want 2238", cfg.FT8Ctrl.WSJTPort)
	}
	if cfg.FT8Ctrl.TXRetries != 5 {
		t.Errorf("TXRetries = %d, want 5", cfg.FT8Ctrl.TXRetries)
	}
	if want := []string{"Any"}; !reflect.DeepEqual([]string(cfg.FT8Ctrl.CallSelector), want) {
		t.Errorf("CallSelector = %v, want %v", cfg.FT8Ctrl.CallSelector, want)
	}

	if !containsStr(cfg.BlackList, "KC5TT") {
		t.Errorf("BlackList = %v, want it to contain KC5TT (uppercase)", cfg.BlackList)
	}

	cq, ok := cfg.Selectors["CQZone"]
	if !ok {
		t.Fatal("Selectors missing CQZone")
	}
	if want := []string{"14", "11", "8", "4", "9"}; !reflect.DeepEqual([]string(cq.List), want) {
		t.Errorf("CQZone.List = %v, want %v (strings)", cq.List, want)
	}

	any, ok := cfg.Selectors["Any"]
	if !ok {
		t.Fatal("Selectors missing Any")
	}
	if !any.LOTWUsersOnly {
		t.Error("Any.LOTWUsersOnly = false, want true")
	}

	extra, ok := cfg.Selectors["Extra"]
	if !ok {
		t.Fatal("Selectors missing Extra")
	}
	if extra.MinSNR == nil {
		t.Fatal("Extra.MinSNR is nil, want the single configured value")
	}
	if *extra.MinSNR != 1 {
		t.Errorf("Extra.MinSNR = %d, want 1 (the last/only duplicate value)", *extra.MinSNR)
	}
}

func TestSingleStringCallSelector(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ft8ctrl.yaml")
	yaml := "" +
		"ft8ctrl:\n" +
		"  my_call: W6BSD\n" +
		"  call_selector: Any\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if want := []string{"Any"}; !reflect.DeepEqual([]string(cfg.FT8Ctrl.CallSelector), want) {
		t.Errorf("CallSelector = %v, want %v (single string -> one-element list)", cfg.FT8Ctrl.CallSelector, want)
	}
}

func TestDBNameTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "ft8ctrl.yaml")
	yaml := "" +
		"ft8ctrl:\n" +
		"  db_name: ~/ft8ctl.sql\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := filepath.Join(home, "ft8ctl.sql")
	if cfg.FT8Ctrl.DBName != want {
		t.Errorf("DBName = %q, want %q", cfg.FT8Ctrl.DBName, want)
	}
}

func containsStr(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
