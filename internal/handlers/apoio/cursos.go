package apoio

func ValoresRemovidos(atuais, novos []string) []string {
	permitidos := make(map[string]struct{}, len(novos))
	for _, v := range novos {
		permitidos[v] = struct{}{}
	}

	removidos := make([]string, 0)
	for _, v := range atuais {
		if _, ok := permitidos[v]; !ok {
			removidos = append(removidos, v)
		}
	}
	return removidos
}
