module drivelocal-host-helper

go 1.22

require gopkg.in/yaml.v3 v3.0.1

// Go 1.23+ muda o default de GODEBUG winsymlink para 1; com winsymlink=1, filepath.EvalSymlinks
// deixa de resolver junctions (mount points) no Windows, e ResolveSelectable precisa do destino real
// para bloquear junctions que apontam para pastas protegidas. Fixado em 0 para não depender da linha go.
godebug winsymlink=0
