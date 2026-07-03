package fixture

func mergeTallies(dst,src map[string]int)map[string]int{
	for k,v:=range src{
		old,ok:=dst[k]
		if ok{
			dst[k]=old+v
		}else{
			dst[k]=v
		}
	}
	return dst
}
