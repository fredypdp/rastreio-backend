package estudantes

import (
	"testing"

	"spuri/internal/handlers/testsupport"
)

func TestSolicitacaoEdicaoHandlersReserveAndReleasePendingGuard(t *testing.T) {
	t.Parallel()

	source := testsupport.ReadHandlerSource(t, "internal/handlers/estudantes/solicitacao_edicao_dado_estudante_handlers.go")
	testsupport.MustContain(t, source, `Reserve(`)
	testsupport.MustContain(t, source, `"solicitacao_edicao_dado_estudante:pendente"`)
	testsupport.MustContain(t, source, `db.CanonicalGuardKey(est.CodigoEstudante, campo)`)
	testsupport.MustContain(t, source, `errors.Is(err, db.ErrUniqueOperationInProgress)`)
	testsupport.MustContain(t, source, `guard.Release()`)
	testsupport.MustContain(t, source, `guard.Consume(agg.GetID())`)
	testsupport.MustContain(t, source, `ReleaseKey("solicitacao_edicao_dado_estudante:pendente", db.CanonicalGuardKey(sol.CodigoEstudante, sol.Campo))`)
}

func TestStatusAcademicoHandlersReserveAndReleasePendingGuard(t *testing.T) {
	t.Parallel()

	source := testsupport.ReadHandlerSource(t, "internal/handlers/academias/academia_status_escolar_handlers.go")
	testsupport.MustContain(t, source, `Reserve(`)
	testsupport.MustContain(t, source, `"solicitacao_status_academico:pendente"`)
	testsupport.MustContain(t, source, `db.CanonicalGuardKey(estudanteDTO.CodigoEstudante, academiaCodigo, tipo)`)
	testsupport.MustContain(t, source, `errors.Is(err, db.ErrUniqueOperationInProgress)`)
	testsupport.MustContain(t, source, `guard.Release()`)
	testsupport.MustContain(t, source, `guard.Consume(uuid.Nil)`)
	testsupport.MustContain(t, source, `ReleaseKey("solicitacao_status_academico:pendente", db.CanonicalGuardKey(codigoEst, codigoAcademia, tipo))`)
}

func TestStudentCreationHandlersReserveBIUniquenessGuard(t *testing.T) {
	t.Parallel()

	direct := testsupport.ReadHandlerSource(t, "internal/handlers/estudantes/estudante_handlers.go")
	testsupport.MustContain(t, direct, `"estudante:bilhete_identidade"`)
	testsupport.MustContain(t, direct, `db.CanonicalGuardKey(*bilhetePtr)`)
	testsupport.MustContain(t, direct, `errors.Is(err, db.ErrUniqueOperationInProgress)`)
	testsupport.MustContain(t, direct, `biGuard.Release()`)
	testsupport.MustContain(t, direct, `biGuard.Consume(estudante.GetID())`)

}
