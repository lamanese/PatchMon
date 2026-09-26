import xml.etree.ElementTree as ET, sys, json
NS=lambda t:t.split('}')[-1]
root=ET.parse('idml/Spreads/Spread_uc8.xml').getroot()
def mat(s):
    a,b,c,d,tx,ty=map(float,s.split()); return (a,b,c,d,tx,ty)
def mul(p,c):  # apply child c then parent p : p*c
    a1,b1,c1,d1,e1,f1=p; a2,b2,c2,d2,e2,f2=c
    return (a1*a2+c1*b2, b1*a2+d1*b2, a1*c2+c1*d2, b1*c2+d1*d2, a1*e2+c1*f2+e1, b1*e2+d1*f2+f1)
def ap(m,x,y):
    a,b,c,d,e,f=m; return (a*x+c*y+e, b*x+d*y+f)
items=[]
def walk(el, m):
    for ch in el:
        t=NS(ch.tag)
        if t=='Group':
            walk(ch, mul(m, mat(ch.get('ItemTransform'))))
        elif t=='Polygon':
            pm=mul(m, mat(ch.get('ItemTransform')))
            paths=[]
            for pg in ch.iter():
                if NS(pg.tag)=='GeometryPathType':
                    pts=[]
                    for p in pg.iter():
                        if NS(p.tag)=='PathPointType':
                            A=ap(pm,*map(float,p.get('Anchor').split())); L=ap(pm,*map(float,p.get('LeftDirection').split())); R=ap(pm,*map(float,p.get('RightDirection').split()))
                            pts.append((A,L,R))
                    paths.append((pg.get('PathOpen')=='true', pts))
            items.append(dict(id=ch.get('Self'), fill=ch.get('FillColor'), tint=ch.get('FillTint') or '100', paths=paths))
        else:
            walk(ch, m)
walk(root,(1,0,0,1,0,0))
def d_of(paths):
    out=[]
    for op,pts in paths:
        n=len(pts); s=f"M{pts[0][0][0]:.3f},{pts[0][0][1]:.3f}"
        rng=range(1,n) if op else range(1,n+1)
        for i in rng:
            p0=pts[i-1]; p1=pts[i%n]
            s+=f" C{p0[2][0]:.3f},{p0[2][1]:.3f} {p1[1][0]:.3f},{p1[1][1]:.3f} {p1[0][0]:.3f},{p1[0][1]:.3f}"
        if not op: s+=" Z"
        out.append(s)
    return " ".join(out)
def bbox(sel):
    xs=[];ys=[]
    for it in sel:
        for op,pts in it['paths']:
            for A,L,R in pts:
                for x,y in (A,L,R): xs.append(x); ys.append(y)
    return min(xs),min(ys),max(xs),max(ys)
ROLE={'ud9':'it','udb':'aman','udc':'claim','udd':'d50','ude':'d50','udf':'d50','ue0':'d100','ue1':'d100','ue2':'d80','ue3':'d80','ue4':'orbit'}
PAL={
 'light': {'aman':'#000000','it':'#2a3441','claim':'#000000','d100':'#2a3441','d80':'#434e5f','d50':'#727d8f','orbit':'#727d8f'},
 'dark':  {'aman':'#ffffff','it':'#c9d3e0','claim':'#ffffff','d100':'#6b7a90','d80':'#8f9cb0','d50':'#b8c2d0','orbit':'#b8c2d0'},
}
def svg(name, roles, theme, pad=2.0):
    sel=[it for it in items if ROLE[it['id']] in roles]
    x0,y0,x1,y1=bbox(sel); x0-=pad;y0-=pad;x1+=pad;y1+=pad
    w=x1-x0; h=y1-y0
    body="".join(f'<path fill="{PAL[theme][ROLE[it["id"]]]}" d="{d_of(it["paths"])}"/>\n' for it in sel)
    s=f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{x0:.3f} {y0:.3f} {w:.3f} {h:.3f}" width="{w:.1f}" height="{h:.1f}">\n{body}</svg>\n'
    open(name,'w').write(s); print(name, f"{w:.1f}x{h:.1f}")
ALL={'aman','it','claim','d50','d100','d80','orbit'}
svg('logo_full_light.svg', ALL, 'light')
svg('logo_full_dark.svg', ALL, 'dark')
svg('logo_wordmark_light.svg', ALL-{'claim'}, 'light')
svg('logo_wordmark_dark.svg', ALL-{'claim'}, 'dark')
svg('logo_icon_light.svg', {'d50','d100','d80','orbit'}, 'light')
svg('logo_icon_dark.svg', {'d50','d100','d80','orbit'}, 'dark')
svg('logo_diamond_light.svg', {'d50','d100','d80'}, 'light')
