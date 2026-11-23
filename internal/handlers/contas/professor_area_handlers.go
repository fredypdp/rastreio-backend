package contas

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"

	"spuri/internal/handlers/apoio"
	"spuri/internal/middleware"
	"spuri/internal/utils"
)

// Área do professor (Plano, Parte 4): consulta SOMENTE das turmas e matérias
// a que está vinculado. Não reaproveita os handlers da academia: cada consulta
// nasce já filtrada pelos vínculos ativos, então não há como "esquecer" um
// filtro. Lançar e corrigir notas e faltas é a Parte 11.

const limiteConsultaProfessor = 2000

func professorDaRequisicao(c *gin.Context) (uuid.UUID, string, bool) {
	id, ok := middleware.GetUserID(c)
	if !ok {
		utils.RespondWithUnauthorizedError(c)
		return uuid.Nil, "", false
	}
	cod, _ := c.Get("codigo_academia")
	codigo, _ := cod.(string)
	if codigo == "" {
		utils.RespondWithForbiddenError(c, "professor sem academia")
		return uuid.Nil, "", false
	}
	return id, codigo, true
}

// materiasDoProfessorNaTurma devolve os ids das matérias vinculadas na turma.
func materiasDoProfessorNaTurma(c *gin.Context, professorID uuid.UUID, codigoTurma string) ([]string, error) {
	rows, err := apoio.GetDbClient(c).DB().Query(`
		SELECT materia_id::text FROM projection_professor_vinculos
		 WHERE professor_id=$1 AND codigo_turma=$2 AND removido_em IS NULL`, professorID, codigoTurma)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListarMeusVinculos — GET /professor/vinculos
func ListarMeusVinculos(c *gin.Context) {
	profID, _, ok := professorDaRequisicao(c)
	if !ok {
		return
	}
	rows, err := apoio.GetDbClient(c).DB().Query(`
		SELECT v.id, v.codigo_turma, v.materia_id, COALESCE(m.nome,''), COALESCE(t.nivel,''), COALESCE(t.turno,''), COALESCE(t.status,'')
		  FROM projection_professor_vinculos v
		  LEFT JOIN projection_materias m ON m.id = v.materia_id
		  LEFT JOIN projection_turmas t ON t.codigo_turma = v.codigo_turma AND t.codigo_academia = v.codigo_academia
		 WHERE v.professor_id = $1 AND v.removido_em IS NULL
		 ORDER BY v.codigo_turma, m.nome`, profID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	defer rows.Close()
	saida := []gin.H{}
	for rows.Next() {
		var id, materiaID uuid.UUID
		var turma, materia, nivel, turno, status string
		if err := rows.Scan(&id, &turma, &materiaID, &materia, &nivel, &turno, &status); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		saida = append(saida, gin.H{"id": id, "codigo_turma": turma, "materia_id": materiaID, "materia_nome": materia, "nivel": nivel, "turno": turno, "status_turma": status})
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// ListarMinhasTurmas — GET /professor/turmas
func ListarMinhasTurmas(c *gin.Context) {
	profID, _, ok := professorDaRequisicao(c)
	if !ok {
		return
	}
	rows, err := apoio.GetDbClient(c).DB().Query(`
		SELECT t.codigo_turma, t.nivel, t.turno, t.status, COALESCE(array_length(t.estudantes,1),0)
		  FROM projection_turmas t
		 WHERE (t.codigo_academia, t.codigo_turma) IN (
		       SELECT codigo_academia, codigo_turma FROM projection_professor_vinculos
		        WHERE professor_id = $1 AND removido_em IS NULL)
		 ORDER BY t.codigo_turma`, profID)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return
	}
	defer rows.Close()
	saida := []gin.H{}
	for rows.Next() {
		var codigo, nivel, turno, status string
		var total int
		if err := rows.Scan(&codigo, &nivel, &turno, &status, &total); err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		saida = append(saida, gin.H{"codigo_turma": codigo, "nivel": nivel, "turno": turno, "status": status, "total_estudantes": total})
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// turmaDoProfessor valida que o professor tem vínculo na turma e devolve os
// códigos dos estudantes e as matérias vinculadas.
func turmaDoProfessor(c *gin.Context) (codigoAcademia string, estudantes []string, materias []string, profID uuid.UUID, ok bool) {
	profID, codigoAcademia, ok = professorDaRequisicao(c)
	if !ok {
		return
	}
	codigoTurma := strings.TrimSpace(c.Param("codigo"))
	materias, err := materiasDoProfessorNaTurma(c, profID, codigoTurma)
	if err != nil {
		utils.RespondWithInternalError(c, err)
		return "", nil, nil, uuid.Nil, false
	}
	if len(materias) == 0 {
		// Mesma resposta para "não existe" e "não é sua".
		utils.RespondWithNotFoundError(c, "turma")
		return "", nil, nil, uuid.Nil, false
	}
	turma, err := apoio.GetTurmasProjection(c).GetByCodigoTurma(codigoTurma, codigoAcademia)
	if err != nil || turma == nil {
		utils.RespondWithNotFoundError(c, "turma")
		return "", nil, nil, uuid.Nil, false
	}
	return codigoAcademia, turma.Estudantes, materias, profID, true
}

// ListarEstudantesDaTurma — GET /professor/turmas/:codigo/estudantes
func ListarEstudantesDaTurma(c *gin.Context) {
	_, estudantes, _, _, ok := turmaDoProfessor(c)
	if !ok {
		return
	}
	saida := []gin.H{}
	if len(estudantes) > 0 {
		rows, err := apoio.GetDbClient(c).DB().Query(`
			SELECT codigo_estudante, nome, status FROM projection_estudantes
			 WHERE codigo_estudante = ANY($1) AND status <> 'deletado' ORDER BY nome`, pq.Array(estudantes))
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var codigo, nome, status string
			if err := rows.Scan(&codigo, &nome, &status); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			saida = append(saida, gin.H{"codigo_estudante": codigo, "nome": nome, "status": status})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// materiasPermitidas aplica ?materia_id= (opcional) sobre as matérias vinculadas.
func materiasPermitidas(c *gin.Context, vinculadas []string) ([]string, bool) {
	pedida := strings.TrimSpace(c.Query("materia_id"))
	if pedida == "" {
		return vinculadas, true
	}
	if _, err := uuid.Parse(pedida); err != nil {
		utils.RespondWithValidationError(c, fmt.Errorf("materia_id inválido"))
		return nil, false
	}
	for _, m := range vinculadas {
		if strings.EqualFold(m, pedida) {
			return []string{m}, true
		}
	}
	utils.RespondWithForbiddenError(c, "você não leciona esta matéria nesta turma")
	return nil, false
}

// ConsultarNotasDaTurma — GET /professor/turmas/:codigo/notas
func ConsultarNotasDaTurma(c *gin.Context) {
	codigoAcademia, estudantes, vinculadas, _, ok := turmaDoProfessor(c)
	if !ok {
		return
	}
	materias, ok := materiasPermitidas(c, vinculadas)
	if !ok {
		return
	}
	saida := []gin.H{}
	if len(estudantes) > 0 {
		args := []interface{}{codigoAcademia, pq.Array(estudantes), pq.Array(materias)}
		q := `SELECT id, codigo_estudante, materia_disciplinar_id, ano_lectivo, periodo, tipo, categoria, nota::float8, ano_academico, registered_at
		        FROM projection_notas
		       WHERE codigo_academia=$1 AND codigo_estudante = ANY($2) AND materia_disciplinar_id::text = ANY($3)`
		if al := strings.TrimSpace(c.Query("ano_lectivo")); al != "" {
			args = append(args, al)
			q += fmt.Sprintf(" AND ano_lectivo=$%d", len(args))
		}
		q += fmt.Sprintf(" ORDER BY codigo_estudante, ano_lectivo, periodo, categoria LIMIT %d", limiteConsultaProfessor)
		rows, err := apoio.GetDbClient(c).DB().Query(q, args...)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id, materiaID uuid.UUID
			var cod, anoLetivo, periodo, tipo, categoria string
			var nota float64
			var anoAcad sql.NullString
			var registadoEm interface{}
			if err := rows.Scan(&id, &cod, &materiaID, &anoLetivo, &periodo, &tipo, &categoria, &nota, &anoAcad, &registadoEm); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			saida = append(saida, gin.H{"id": id, "codigo_estudante": cod, "materia_id": materiaID, "ano_lectivo": anoLetivo, "periodo": periodo,
				"tipo": tipo, "categoria": categoria, "nota": nota, "ano_academico": anoAcad.String, "registered_at": registadoEm})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}

// ConsultarFaltasDaTurma — GET /professor/turmas/:codigo/faltas
func ConsultarFaltasDaTurma(c *gin.Context) {
	codigoAcademia, estudantes, vinculadas, _, ok := turmaDoProfessor(c)
	if !ok {
		return
	}
	materias, ok := materiasPermitidas(c, vinculadas)
	if !ok {
		return
	}
	saida := []gin.H{}
	if len(estudantes) > 0 {
		args := []interface{}{codigoAcademia, pq.Array(estudantes), pq.Array(materias)}
		q := `SELECT id, codigo_estudante, materia_disciplinar_id, ano_lectivo, to_char(data,'YYYY-MM-DD'), quantidade, periodo, sumario_titulo
		        FROM projection_faltas
		       WHERE codigo_academia=$1 AND codigo_estudante = ANY($2) AND materia_disciplinar_id::text = ANY($3)`
		if al := strings.TrimSpace(c.Query("ano_lectivo")); al != "" {
			args = append(args, al)
			q += fmt.Sprintf(" AND ano_lectivo=$%d", len(args))
		}
		q += fmt.Sprintf(" ORDER BY codigo_estudante, data DESC LIMIT %d", limiteConsultaProfessor)
		rows, err := apoio.GetDbClient(c).DB().Query(q, args...)
		if err != nil {
			utils.RespondWithInternalError(c, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id, materiaID uuid.UUID
			var cod, anoLetivo, data string
			var quantidade int
			var periodo, sumario sql.NullString
			if err := rows.Scan(&id, &cod, &materiaID, &anoLetivo, &data, &quantidade, &periodo, &sumario); err != nil {
				utils.RespondWithInternalError(c, err)
				return
			}
			saida = append(saida, gin.H{"id": id, "codigo_estudante": cod, "materia_id": materiaID, "ano_lectivo": anoLetivo, "data": data,
				"quantidade": quantidade, "periodo": periodo.String, "sumario_titulo": sumario.String})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": saida, "total": len(saida)})
}
