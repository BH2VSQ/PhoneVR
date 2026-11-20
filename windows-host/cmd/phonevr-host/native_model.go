package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

//go:embed ui/assets/daydream_view_2017.obj ui/assets/daydream_view_2017.mtl
var nativeModelAssets embed.FS

type modelVec3 struct{ X, Y, Z float32 }
type modelTriangle struct {
	P [3]modelVec3
	N [3]modelVec3
	C [3]float32
}
type nativeHeadsetModel struct {
	Triangles []modelTriangle
	Center    modelVec3
	Scale     float32
}

func loadNativeHeadsetModel() (*nativeHeadsetModel, error) {
	obj, err := nativeModelAssets.Open("ui/assets/daydream_view_2017.obj")
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	mtl, err := nativeModelAssets.Open("ui/assets/daydream_view_2017.mtl")
	if err != nil {
		return nil, err
	}
	defer mtl.Close()
	materials, err := parseNativeMTL(mtl)
	if err != nil {
		return nil, err
	}
	return parseNativeOBJ(obj, materials)
}

func parseNativeMTL(r io.Reader) (map[string][3]float32, error) {
	materials := make(map[string][3]float32)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	current := ""
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "newmtl":
			if len(fields) > 1 {
				current = fields[1]
			}
		case "Kd":
			if current != "" && len(fields) >= 4 {
				r, e1 := strconv.ParseFloat(fields[1], 32)
				g, e2 := strconv.ParseFloat(fields[2], 32)
				b, e3 := strconv.ParseFloat(fields[3], 32)
				if e1 == nil && e2 == nil && e3 == nil {
					materials[current] = [3]float32{float32(r), float32(g), float32(b)}
				}
			}
		}
	}
	return materials, scanner.Err()
}

func parseNativeOBJ(r io.Reader, materials map[string][3]float32) (*nativeHeadsetModel, error) {
	vertices := make([]modelVec3, 0, 50000)
	normals := make([]modelVec3, 0, 50000)
	triangles := make([]modelTriangle, 0, 100000)
	currentColor := [3]float32{0.45, 0.48, 0.50}
	minV := modelVec3{X: 1e9, Y: 1e9, Z: 1e9}
	maxV := modelVec3{X: -1e9, Y: -1e9, Z: -1e9}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	parseVec := func(fields []string) (modelVec3, error) {
		if len(fields) < 4 {
			return modelVec3{}, fmt.Errorf("invalid OBJ vector")
		}
		values := [3]float32{}
		for i := 0; i < 3; i++ {
			value, err := strconv.ParseFloat(fields[i+1], 32)
			if err != nil {
				return modelVec3{}, err
			}
			values[i] = float32(value)
		}
		return modelVec3{values[0], values[1], values[2]}, nil
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "v":
			v, err := parseVec(fields)
			if err != nil {
				return nil, err
			}
			vertices = append(vertices, v)
			if v.X < minV.X {
				minV.X = v.X
			}
			if v.Y < minV.Y {
				minV.Y = v.Y
			}
			if v.Z < minV.Z {
				minV.Z = v.Z
			}
			if v.X > maxV.X {
				maxV.X = v.X
			}
			if v.Y > maxV.Y {
				maxV.Y = v.Y
			}
			if v.Z > maxV.Z {
				maxV.Z = v.Z
			}
		case "vn":
			n, err := parseVec(fields)
			if err != nil {
				return nil, err
			}
			normals = append(normals, n)
		case "usemtl":
			if len(fields) > 1 {
				if color, ok := materials[fields[1]]; ok {
					currentColor = color
				}
			}
		case "f":
			if len(fields) < 4 {
				continue
			}
			face := make([][2]int, 0, len(fields)-1)
			for _, token := range fields[1:] {
				parts := strings.Split(token, "/")
				if len(parts) == 0 {
					continue
				}
				vi, err := strconv.Atoi(parts[0])
				if err != nil {
					return nil, err
				}
				if vi < 0 {
					vi = len(vertices) + vi + 1
				}
				ni := 0
				if len(parts) >= 3 && parts[2] != "" {
					ni, _ = strconv.Atoi(parts[2])
					if ni < 0 {
						ni = len(normals) + ni + 1
					}
				}
				face = append(face, [2]int{vi - 1, ni - 1})
			}
			for i := 1; i+1 < len(face); i++ {
				indices := [3][2]int{face[0], face[i], face[i+1]}
				tri := modelTriangle{C: currentColor}
				valid := true
				for j, pair := range indices {
					if pair[0] < 0 || pair[0] >= len(vertices) {
						valid = false
						break
					}
					tri.P[j] = vertices[pair[0]]
					if pair[1] >= 0 && pair[1] < len(normals) {
						tri.N[j] = normals[pair[1]]
					}
				}
				if valid {
					triangles = append(triangles, tri)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(vertices) == 0 || len(triangles) == 0 {
		return nil, fmt.Errorf("OBJ model contains no renderable geometry")
	}
	center := modelVec3{(minV.X + maxV.X) / 2, (minV.Y + maxV.Y) / 2, (minV.Z + maxV.Z) / 2}
	dx, dy, dz := maxV.X-minV.X, maxV.Y-minV.Y, maxV.Z-minV.Z
	largest := maxf(dx, maxf(dy, dz))
	if largest <= 0 {
		largest = 1
	}
	scale := float32(1.35) / largest
	for i := range triangles {
		for j := 0; j < 3; j++ {
			p := triangles[i].P[j]
			triangles[i].P[j] = modelVec3{(p.X - center.X) * scale, (p.Y - center.Y) * scale, (p.Z - center.Z) * scale}
			n := triangles[i].N[j]
			triangles[i].N[j] = normalizeModelVec(n)
		}
	}
	return &nativeHeadsetModel{Triangles: triangles, Center: center, Scale: scale}, nil
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func normalizeModelVec(v modelVec3) modelVec3 {
	length := float32(1)
	length = float32(float64(v.X*v.X + v.Y*v.Y + v.Z*v.Z))
	if length <= 1e-12 {
		return modelVec3{0, 0, 1}
	}
	inv := 1 / float32Sqrt(length)
	return modelVec3{v.X * inv, v.Y * inv, v.Z * inv}
}
func float32Sqrt(v float32) float32 { return float32(math.Sqrt(float64(v))) }
