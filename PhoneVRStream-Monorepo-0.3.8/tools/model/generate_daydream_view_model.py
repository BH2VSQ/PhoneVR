import math, os, json, hashlib
from pathlib import Path
import numpy as np
import trimesh
from PIL import Image

REPO_ROOT=Path(__file__).resolve().parents[2]
OUT=REPO_ROOT/'windows-host/cmd/phonevr-host/ui/assets'
OUT.mkdir(parents=True, exist_ok=True)

# Material palette derived from the 2017 Daydream View Fog/Charcoal industrial design.
COLORS={
 'fabric_shell': (0.43,0.45,0.46),
 'fabric_front': (0.48,0.50,0.50),
 'fabric_seam': (0.31,0.33,0.34),
 'plastic_trim': (0.12,0.14,0.15),
 'plastic_latch': (0.25,0.27,0.28),
 'foam_pad': (0.045,0.055,0.061),
 'inner_frame': (0.085,0.10,0.112),
 'lens_rim': (0.20,0.23,0.24),
 'lens_glass': (0.075,0.19,0.23),
 'lens_reflection': (0.34,0.50,0.52),
 'logo': (0.78,0.81,0.79),
 'metal': (0.48,0.51,0.52),
}

meshes=[]

def add(name, mesh, color=None, metal=0.0, rough=0.7):
    if mesh.is_empty: return
    mesh.remove_unreferenced_vertices()
    try:
        mesh.fix_normals()
    except Exception: pass
    meshes.append((name, mesh, color or COLORS.get(name, (0.4,0.4,0.4)), metal, rough))
    return mesh

def superellipsoid(center, scale, e1=0.65, e2=0.65, nlat=48, nlon=96):
    cx,cy,cz=center; a,b,c=scale
    verts=[]
    for i in range(nlat+1):
        lat=-math.pi/2 + math.pi*i/nlat
        cl=math.cos(lat); sl=math.sin(lat)
        clp=math.copysign(abs(cl)**e1, cl)
        slp=math.copysign(abs(sl)**e1, sl)
        for j in range(nlon):
            lon=2*math.pi*j/nlon
            cp=math.cos(lon); sp=math.sin(lon)
            x=a*clp*math.copysign(abs(cp)**e2,cp)+cx
            y=b*clp*math.copysign(abs(sp)**e2,sp)+cy
            z=c*slp+cz
            verts.append((x,y,z))
    faces=[]
    for i in range(nlat):
        for j in range(nlon):
            a0=i*nlon+j; a1=i*nlon+(j+1)%nlon
            b0=(i+1)*nlon+j; b1=(i+1)*nlon+(j+1)%nlon
            faces.extend([(a0,a1,b0),(a1,b1,b0)])
    m=trimesh.Trimesh(vertices=np.asarray(verts),faces=np.asarray(faces),process=True)
    if m.volume < 0: m.invert()
    return m

def rounded_box(center, size, e=0.55, res=28):
    return superellipsoid(center, tuple(v/2 for v in size), e,e,res,res*2)

def tube_between(p0,p1,radius,sections=20):
    p0=np.array(p0,float); p1=np.array(p1,float)
    axis=p1-p0; length=np.linalg.norm(axis)
    m=trimesh.creation.cylinder(radius=radius,height=length,sections=sections)
    m.apply_transform(trimesh.geometry.align_vectors([0,0,1],axis))
    m.apply_translation((p0+p1)/2)
    return m

def torus(center, major, minor, axis='z', nu=56, nv=16):
    verts=[]; faces=[]
    for i in range(nu):
        u=2*math.pi*i/nu
        for j in range(nv):
            v=2*math.pi*j/nv
            rr=major+minor*math.cos(v)
            x=rr*math.cos(u); y=rr*math.sin(u); z=minor*math.sin(v)
            if axis=='x': x,z=z,x
            elif axis=='y': y,z=z,y
            verts.append((x+center[0],y+center[1],z+center[2]))
    for i in range(nu):
        for j in range(nv):
            a=i*nv+j; b=i*nv+(j+1)%nv; c=((i+1)%nu)*nv+j; d=((i+1)%nu)*nv+(j+1)%nv
            faces.extend([(a,c,b),(b,c,d)])
    m=trimesh.Trimesh(vertices=np.array(verts),faces=np.array(faces),process=True)
    if m.volume<0: m.invert()
    return m

def add_sphere(name, center, scale, color, rough=.4, metal=0):
    m=trimesh.creation.icosphere(subdivisions=3, radius=1)
    m.apply_scale(scale); m.apply_translation(center)
    add(name,m,color,metal,rough)

# Main soft fabric visor body. Rounded, shallow, slightly curved silhouette.
add('fabric_shell', superellipsoid((0,0,0.0),(0.86,0.47,0.46),0.60,0.62,64,128), COLORS['fabric_shell'],0,0.92)

# Front phone-cover flap: a broad fabric-covered rounded panel, slightly proud of the shell.
add('fabric_front', superellipsoid((0,0.0,-0.445),(0.805,0.405,0.085),0.48,0.54,48,112), COLORS['fabric_front'],0,0.94)
# Narrow dark seam around the cover perimeter represented as soft rounded piping.
# Upper and lower transverse seams.
# Left/right edge piping.

# Front-cover hook and release latch, characteristic of the 2017 flap arrangement.
add('plastic_trim', rounded_box((0,0.462,-0.235),(0.235,0.082,0.13),0.38), COLORS['plastic_trim'],0.08,0.38)
add('plastic_latch', rounded_box((0,0.493,-0.303),(0.12,0.026,0.025),0.5), COLORS['plastic_latch'],0.05,0.35)
# Small flush vent details around the phone-cover thermal path.
for idx,x in enumerate([-0.20,-0.07,0.07,0.20]):
    add('plastic_trim', rounded_box((x,-0.348,-0.485),(0.075,0.012,0.009),0.85,14), COLORS['plastic_trim'])

# Side trim and strap attachment mounts only; no straps/headband are modeled.
# Small shallow side strap-anchor bosses are integrated into the body edge. No straps are included.
for sign in (-1,1):
    add('fabric_seam', rounded_box((sign*0.848,0.09,0.08),(0.045,0.13,0.14),0.68), COLORS['fabric_seam'],0,0.82)

# Small side Google-style G badge on the visible right side, as a subtle relief rather than a huge logo.
add('plastic_trim', tube_between((0.865,0.015,-0.12),(0.89,0.015,-0.12),0.09), COLORS['plastic_trim'],0.05,0.4)
add('logo', torus((0.902,0.015,-0.12),0.052,0.009,axis='x',nu=40,nv=10), COLORS['logo'],0.12,0.42)
add('logo', rounded_box((0.910,0.012,-0.12),(0.022,0.018,0.078),0.4,16), COLORS['logo'],0.1,0.4)

# Rear wearer-side optical housing: dark rigid frame plus soft face cushion.
add('inner_frame', superellipsoid((0,0,0.425),(0.805,0.415,0.115),0.58,0.58,48,112), COLORS['inner_frame'],0.08,0.45)
# Face pad is a thick, dark foam surround; the four separated pieces preserve the eye opening.
add('foam_pad', rounded_box((0,0.355,0.505),(1.38,0.085,0.13),0.75), COLORS['foam_pad'])
add('foam_pad', rounded_box((0,-0.355,0.505),(1.38,0.085,0.13),0.75), COLORS['foam_pad'])
add('foam_pad', rounded_box((-0.68,0,0.505),(0.095,0.70,0.13),0.75), COLORS['foam_pad'])
add('foam_pad', rounded_box((0.68,0,0.505),(0.095,0.70,0.13),0.75), COLORS['foam_pad'])

# Nose bridge / cheek relief on wearer side.
add('foam_pad', rounded_box((0,-0.15,0.585),(0.13,0.16,0.075),0.55), COLORS['foam_pad'])

# Two optical modules are only on the rear (+Z, wearer eye side).
for idx,x in enumerate((-0.385,0.385)):
    # Structural barrel and stepped annular lens seats.
    add('lens_barrel', rounded_box((x,0.015,0.605),(0.435,0.435,0.17),0.72), (0.075,0.09,0.10),0.08,0.52)
    add('lens_rim', torus((x,0.015,0.705),0.179,0.031,'z'), (0.36,0.39,0.39),0.32,0.24)
    add('lens_inner', torus((x,0.015,0.711),0.145,0.012,'z'), (0.13,0.18,0.19),0.25,0.2)
    # Slightly convex dark lens, facing the wearer.
    add_sphere('lens_glass',(x,0.015,0.706),(0.147,0.147,0.035),(0.07,0.22,0.26),.16,.26)
    add_sphere('lens_reflection',(x-0.045,0.067,0.739),(0.025,0.018,0.006),(0.58,0.72,0.71),.1,.3)

# Rear alignment posts and inner phone guides (Daydream View 2017 front-cover alignment features).
for sign in (-1,1):
    add('plastic_latch', rounded_box((sign*0.66,0.31,0.53),(0.055,0.16,0.12),0.45), COLORS['plastic_latch'],0.05,0.4)
    add('plastic_latch', rounded_box((sign*0.66,-0.31,0.53),(0.055,0.16,0.12),0.45), COLORS['plastic_latch'],0.05,0.4)

# Subtle heathered woven textile texture for the 2017 fabric shell and cover.
rng=np.random.default_rng(2017)
tex_size=256
base=np.zeros((tex_size,tex_size,3),dtype=np.float32)
base[:]=np.array([119,126,128],dtype=np.float32)
for y in range(tex_size):
    for x in range(tex_size):
        thread = (2.8 if (x % 4 in (0,1)) else -1.8) + (1.4 if (y % 4 in (0,1)) else -1.2)
        noise = rng.normal(0,2.2)
        base[y,x,:] += thread + noise
fabric_image=Image.fromarray(np.uint8(np.clip(base,0,255)),'RGB')
fabric_image.save(OUT/'phonevr_fabric.png')

# Scene export with PBR materials for general-purpose model viewers.
scene=trimesh.Scene()
for i,(name,m,color,metal,rough) in enumerate(meshes):
    rgba=tuple(int(max(0,min(1,v))*255) for v in color)+(255,)
    if name in ('fabric_shell','fabric_front','fabric_seam'):
        mat=trimesh.visual.material.PBRMaterial(name=name,baseColorFactor=(255,255,255,255),baseColorTexture=fabric_image,metallicFactor=0.0,roughnessFactor=0.96)
        center=m.bounds.mean(axis=0); span=np.maximum(m.bounds[1]-m.bounds[0],1e-6)
        rel=m.vertices-center
        u=(np.arctan2(rel[:,1]/span[1],rel[:,0]/span[0])/(2*np.pi)+0.5)%1.0
        v=np.clip(rel[:,2]/(span[2]/2),-1,1)
        v=(np.arcsin(v)/np.pi)+0.5
        uv=np.column_stack((u,v)).astype(np.float32)
        m.visual=trimesh.visual.TextureVisuals(uv=uv,material=mat)
    else:
        mat=trimesh.visual.material.PBRMaterial(name=name,baseColorFactor=rgba,metallicFactor=metal,roughnessFactor=rough)
        m.visual=trimesh.visual.TextureVisuals(material=mat)
    scene.add_geometry(m,geom_name=f'{name}_{i}',node_name=f'{name}_{i}')

# OBJ with groups/material names, computed smooth vertex normals for external DCC applications.
obj_lines=['# PhoneVR Daydream View 2017-inspired headset shell model','mtllib phonevr_hmd.mtl','o daydream_view_2017_headset']
mtl_lines=[]
for name,color in COLORS.items():
    mtl_lines += [f'newmtl {name}',f'Ka {color[0]*0.35:.5f} {color[1]*0.35:.5f} {color[2]*0.35:.5f}',f'Kd {color[0]:.5f} {color[1]:.5f} {color[2]:.5f}','Ks 0.22 0.22 0.22','Ns 60','d 1.0','illum 2'] + (['map_Kd phonevr_fabric.png'] if name in ('fabric_shell','fabric_front','fabric_seam') else []) + ['']
vert_offset=1; normal_offset=1
for index,(name,m,color,metal,rough) in enumerate(meshes):
    # Keep separate named groups and material assignment.
    obj_lines += [f'o {name}_{index}',f'g {name}_{index}',f'usemtl {name}']
    verts=np.asarray(m.vertices); faces=np.asarray(m.faces)
    vnorm=np.asarray(m.vertex_normals)
    for v in verts: obj_lines.append('v %.7f %.7f %.7f'%tuple(v))
    for n in vnorm: obj_lines.append('vn %.7f %.7f %.7f'%tuple(n))
    for f in faces:
        ids=[int(a)+vert_offset for a in f]
        nids=[int(a)+normal_offset for a in f]
        obj_lines.append('f '+' '.join(f'{a}//{b}' for a,b in zip(ids,nids)))
    vert_offset+=len(verts); normal_offset+=len(vnorm)

(OUT/'phonevr_hmd.obj').write_text('\n'.join(obj_lines)+'\n',encoding='utf-8')
(OUT/'phonevr_hmd.mtl').write_text('\n'.join(mtl_lines),encoding='utf-8')
scene.export(OUT/'phonevr_hmd.glb',file_type='glb')

# also expose clearly named model assets alongside compatibility filenames
(OUT/'daydream_view_2017.obj').write_bytes((OUT/'phonevr_hmd.obj').read_bytes())
(OUT/'daydream_view_2017.mtl').write_bytes((OUT/'phonevr_hmd.mtl').read_bytes())
(OUT/'daydream_view_2017.glb').write_bytes((OUT/'phonevr_hmd.glb').read_bytes())
(OUT/'daydream_view_2017_fabric.png').write_bytes((OUT/'phonevr_fabric.png').read_bytes())

# basic integrity and bounds
all_bounds=np.array([m.bounds for _,m,_,_,_ in meshes])
print('mesh components',len(meshes),'triangles',sum(len(m.faces) for _,m,_,_,_ in meshes))
print('bounds',np.min(all_bounds[:,0,:],axis=0),np.max(all_bounds[:,1,:],axis=0))
print('OBJ bytes',(OUT/'phonevr_hmd.obj').stat().st_size,'GLB bytes',(OUT/'phonevr_hmd.glb').stat().st_size)
loaded=trimesh.load(OUT/'phonevr_hmd.glb',force='scene')
print('GLB loaded geometry',len(loaded.geometry))
