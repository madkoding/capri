package skills_test

import (
	"capri/internal/config"
	"context"
	"testing"

	"capri/internal/embed"
	"capri/internal/skills"
)

func TestParseSkillHeaderAndExamples(t *testing.T) {
	s := skills.ParseSkill("---\nname: demo\ndescription: una skill de prueba\n---\nObjetivo: haz algo\nfs_list: .\nResultado: a.txt\nDONE\n")
	if s == nil {
		t.Fatal("ParseSkill devolvió nil")
	}
	if s.Name != "demo" || s.Description != "una skill de prueba" {
		t.Errorf("Skill = %+v", s)
	}
	want := []string{"user", "assistant", "user", "assistant"}
	if len(s.Turns) != len(want) {
		t.Fatalf("turns = %#v", s.Turns)
	}
	for i, role := range want {
		if s.Turns[i].Role != role {
			t.Errorf("turno %d rol %q, quería %q", i, s.Turns[i].Role, role)
		}
	}
	if s.Turns[0].Content != "Objetivo: haz algo" {
		t.Errorf("turno 0 = %q", s.Turns[0].Content)
	}
}

func TestParseSkillRejectsInvalid(t *testing.T) {
	if s := skills.ParseSkill("solo texto"); s != nil {
		t.Errorf("ParseSkill = %+v, quería nil", s)
	}
	if s := skills.ParseSkill("---\nname: x\n---\nObjetivo: y"); s != nil {
		t.Errorf("ParseSkill sin description = %+v, quería nil", s)
	}
}

func TestLibraryHasUniqueNamedSkills(t *testing.T) {
	all := skills.LoadSkills(skills.SkillsDir)
	if len(all) < 10 {
		t.Fatalf("skills = %d, quería >= 10", len(all))
	}
	seen := map[string]bool{}
	for _, s := range all {
		if seen[s.Name] {
			t.Errorf("skill duplicada: %s", s.Name)
		}
		seen[s.Name] = true
		if len(s.Turns) < 2 {
			t.Errorf("skill %s sin ejemplos", s.Name)
		}
	}
}

func TestFindSkillsMatchesGoal(t *testing.T) {
	if !embed.Health(context.Background()) {
		t.Skip("servicio embed no disponible")
	}
	names := func(goal string) []string {
		var out []string
		for _, s := range skills.FindSkills(context.Background(), goal, 5) {
			out = append(out, s.Name)
		}
		return out
	}
	if got := names("cuántas líneas tiene el archivo log.txt"); !has(got, "contar-lineas") {
		t.Errorf("findSkills = %#v", got)
	}
	if got := names("suma los valores de precios.csv"); !has(got, "sumar-columna") {
		t.Errorf("findSkills = %#v", got)
	}
	if config.Get().EmbedFamily == "nomic" {
		t.Log("nomic no separa objetivos ajenos de las skills (medido): se omite la comprobación negativa")
		return
	}
	if got := names("dónde queda la luna"); len(got) != 0 {
		t.Errorf("findSkills(luna) = %#v", got)
	}
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
