package _189pc

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
)

func TestShouldRestoreSourceFromCAS(t *testing.T) {
	driver := &Cloud189PC{Addition: Addition{RestoreSourceFromCAS: true}}
	if !driver.shouldRestoreSourceFromCAS("movie.mkv.cas") {
		t.Fatal("expected .cas restore to be enabled")
	}
	if driver.shouldRestoreSourceFromCAS("movie.mkv") {
		t.Fatal("did not expect non-.cas file to trigger restore")
	}
}

func TestShouldDeleteCASAfterRestore(t *testing.T) {
	driver := &Cloud189PC{Addition: Addition{DeleteCASAfterRestore: true}}
	if !driver.shouldDeleteCASAfterRestore("movie.mkv.cas") {
		t.Fatal("expected .cas cleanup to be enabled")
	}
	if driver.shouldDeleteCASAfterRestore("movie.mkv") {
		t.Fatal("did not expect non-.cas file to trigger cleanup")
	}
}

func TestResolveRestoreSourceName(t *testing.T) {
	tests := []struct {
		name        string
		driver      *Cloud189PC
		casFileName string
		info        *casfile.Info
		want        string
		wantErr     bool
	}{
		{
			name:        "default uses cas payload name",
			driver:      &Cloud189PC{},
			casFileName: "renamed.mkv.cas",
			info:        &casfile.Info{Name: "movie.mkv"},
			want:        "movie.mkv",
		},
		{
			name:        "default still rejects path in payload name",
			driver:      &Cloud189PC{},
			casFileName: "renamed.mkv.cas",
			info:        &casfile.Info{Name: "folder/movie.mkv"},
			wantErr:     true,
		},
		{
			name:        "switch uses current cas file name without suffix",
			driver:      &Cloud189PC{Addition: Addition{RestoreSourceUseCurrentName: true}},
			casFileName: "renamed.mkv.CAS",
			info:        &casfile.Info{Name: "movie.mkv"},
			want:        "renamed.mkv",
		},
		{
			name:        "switch appends source extension when current name has none",
			driver:      &Cloud189PC{Addition: Addition{RestoreSourceUseCurrentName: true}},
			casFileName: "renamed.cas",
			info:        &casfile.Info{Name: "movie.mkv"},
			want:        "renamed.mkv",
		},
		{
			name:        "switch ignores invalid payload name when current name is valid",
			driver:      &Cloud189PC{Addition: Addition{RestoreSourceUseCurrentName: true}},
			casFileName: "renamed.mkv.cas",
			info:        &casfile.Info{Name: "folder/movie.mkv"},
			want:        "renamed.mkv",
		},
		{
			name:        "switch can still reuse payload extension when payload has a path",
			driver:      &Cloud189PC{Addition: Addition{RestoreSourceUseCurrentName: true}},
			casFileName: "renamed.cas",
			info:        &casfile.Info{Name: "folder/movie.mkv"},
			want:        "renamed.mkv",
		},
		{
			name:        "switch rejects empty source name derived from current name",
			driver:      &Cloud189PC{Addition: Addition{RestoreSourceUseCurrentName: true}},
			casFileName: ".cas",
			info:        &casfile.Info{Name: "movie.mkv"},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.driver.resolveRestoreSourceName(tt.casFileName, tt.info)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve restore source name: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
