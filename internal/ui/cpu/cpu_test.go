package cpu_test

import (
	"testing"

	"capri/internal/ui/cpu"
)

func f64(v float64) *float64 { return &v }

func TestShortModel(t *testing.T) {
	cases := map[string]string{
		"Intel(R) Core(TM) i3-6100 CPU @ 3.70GHz": "Intel Core i3-6100",
		"AMD Ryzen 7 5800X 8-Core Processor":      "AMD Ryzen 7 5800X 8-Core Processor",
		"":                                        "",
	}
	for in, want := range cases {
		if got := cpu.ShortModel(in); got != want {
			t.Errorf("ShortModel(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestParseCpuModel(t *testing.T) {
	info := "processor\t: 0\nmodel name\t: Intel(R) Core(TM) i3-6100 CPU @ 3.70GHz\n"
	if got := cpu.ParseCpuModel(info); got != "Intel(R) Core(TM) i3-6100 CPU @ 3.70GHz" {
		t.Errorf("ParseCpuModel = %q", got)
	}
	if got := cpu.ParseCpuModel("sin nada"); got != "" {
		t.Errorf("ParseCpuModel(sin nada) = %q, quería vacío", got)
	}
}

func TestPickPackageTemp(t *testing.T) {
	zones := []cpu.Zone{
		{Type: "acpitz", Temp: 27800, OK: true},
		{Type: "pch_skylake", Temp: 46500, OK: true},
		{Type: "x86_pkg_temp", Temp: 31000, OK: true},
	}
	got := cpu.PickPackageTemp(zones)
	if got == nil || *got != 31 {
		t.Fatalf("PickPackageTemp = %v, quería 31", got)
	}
	if got := cpu.PickPackageTemp([]cpu.Zone{{Type: "acpitz", Temp: 27800, OK: true}}); got != nil {
		t.Errorf("acpitz no debe valer: %v", *got)
	}
	if got := cpu.PickPackageTemp([]cpu.Zone{{Type: "x86_pkg_temp", Temp: f64NaN(), OK: true}}); got != nil {
		t.Errorf("NaN no debe valer: %v", *got)
	}
}

func f64NaN() float64 {
	var z float64
	return z / z
}

func TestReadCpu(t *testing.T) {
	info := cpu.ReadCpu()
	if info == nil {
		t.Fatal("ReadCpu no debe devolver nil en Linux")
	}
	if info.Threads < 1 {
		t.Errorf("Threads = %d", info.Threads)
	}
	if info.Temp != nil && (*info.Temp <= -50 || *info.Temp >= 150) {
		t.Errorf("Temp = %f", *info.Temp)
	}
	_ = f64(0)
}
