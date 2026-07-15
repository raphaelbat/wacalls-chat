import { Fragment, useEffect, useMemo, useState, type ElementType } from "react";
import { History, Loader2, Pencil, Plus, Shield, Trash2, User as UserIcon } from "lucide-react";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import * as authApi from "@/services/auth";
import type { RoleAuditEntry } from "@/services/auth";
import { listQueues } from "@/services/queues";
import { useAuth } from "@/stores/auth";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import type { AuthUser } from "@/types/auth";
import type { Queue } from "@/types/queue";
import { PERMISSIONS, DEFAULT_PERMISSIONS, type Permission } from "@/lib/permissions";
import { ensureQuota } from "@/lib/quota";
import { toastError } from "@/lib/error-toast";
import { useTranslation } from "react-i18next";

type FormState = {
  email: string;
  name: string;
  password: string;
  companyName: string;
  cpf: string;
  role: "admin" | "user";
  queueIds: string[];
  sessionIds: string[];
  permissions: Permission[];
};

const emptyForm: FormState = {
  email: "",
  name: "",
  password: "",
  companyName: "",
  cpf: "",
  role: "admin",
  queueIds: [],
  sessionIds: [],
  permissions: [...DEFAULT_PERMISSIONS],
};

export const AdminUsersPage = ({ embedded = false }: { embedded?: boolean } = {}) => {
  const { t } = useTranslation();
  const me = useAuth((s) => s.user);
  const canManage = !!me?.roles.includes("admin");
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [queues, setQueues] = useState<Queue[]>([]);
  const sessions = useSessions((s) => s.sessions);
  useEffect(() => { ensureSessionsWired(); }, []);
  const [loading, setLoading] = useState(true);
  const [toDelete, setToDelete] = useState<AuthUser | null>(null);
  const [editing, setEditing] = useState<AuthUser | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [roleChange, setRoleChange] = useState<{ user: AuthUser; nextRole: "admin" | "user" } | null>(null);
  const [roleSaving, setRoleSaving] = useState(false);
  const [audit, setAudit] = useState<RoleAuditEntry[]>([]);
  const [auditOpen, setAuditOpen] = useState(false);
  const [auditLoading, setAuditLoading] = useState(false);

  const adminCount = useMemo(() => users.filter((u) => u.roles.includes("admin")).length, [users]);

  const loadAudit = async () => {
    setAuditLoading(true);
    try {
      setAudit(await authApi.listRoleAudit(100));
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setAuditLoading(false);
    }
  };

  useEffect(() => {
    if (auditOpen && canManage) void loadAudit();
  }, [auditOpen, canManage]);

  const queueById = useMemo(() => {
    const m = new Map<string, Queue>();
    queues.forEach((q) => m.set(q.id, q));
    return m;
  }, [queues]);

  const reload = async () => {
    setLoading(true);
    try {
      const [u, q] = await Promise.all([authApi.listUsers(), listQueues()]);
      setUsers(u);
      setQueues(q);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void reload();
  }, []);

  const openCreate = () => {
    setForm({ ...emptyForm, permissions: [...DEFAULT_PERMISSIONS] });
    setCreating(true);
  };

  const openEdit = async (u: AuthUser) => {
    setForm({
      email: u.email,
      name: u.name ?? "",
      password: "",
      companyName: u.companyName ?? "",
      cpf: u.cpf ?? "",
      role: u.roles.includes("admin") ? "admin" : "user",
      queueIds: [],
      sessionIds: [],
      permissions: [],
    });
    setEditing(u);
    try {
      const [ids, perms, sids] = await Promise.all([
        authApi.getUserQueues(u.id),
        authApi.getUserPermissions(u.id),
        authApi.getUserSessions(u.id),
      ]);
      setForm((f) => ({ ...f, queueIds: ids, permissions: perms as Permission[], sessionIds: sids }));
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const toggleQueueInForm = (id: string) =>
    setForm((f) => ({
      ...f,
      queueIds: f.queueIds.includes(id) ? f.queueIds.filter((x) => x !== id) : [...f.queueIds, id],
    }));

  const toggleSessionInForm = (id: string) =>
    setForm((f) => ({
      ...f,
      sessionIds: f.sessionIds.includes(id) ? f.sessionIds.filter((x) => x !== id) : [...f.sessionIds, id],
    }));

  const togglePermissionInForm = (key: Permission) =>
    setForm((f) => ({
      ...f,
      permissions: f.permissions.includes(key)
        ? f.permissions.filter((x) => x !== key)
        : [...f.permissions, key],
    }));

  const setAllPermissions = (on: boolean) =>
    setForm((f) => ({ ...f, permissions: on ? PERMISSIONS.map((p) => p.key) : [] }));

  const submitCreate = async () => {
    if (!ensureQuota("usuarios", users.length)) return;
    setSaving(true);
    try {
      const created = await authApi.createUser({
        email: form.email.trim(),
        password: form.password,
        name: form.name.trim(),
        companyName: form.companyName.trim(),
        cpf: form.cpf.trim(),
        role: form.role,
        queueIds: form.queueIds,
      });
      if (form.role !== "admin") {
        await authApi.setUserPermissions(created.id, form.permissions);
      }
      if (form.sessionIds.length) {
        await authApi.setUserSessions(created.id, form.sessionIds);
      }
      toast.success("Usuário criado");
      setCreating(false);
      await reload();
    } catch (e) {
      toastError(e);
    } finally {
      setSaving(false);
    }
  };

  const submitEdit = async () => {
    if (!editing) return;
    setSaving(true);
    try {
      await authApi.updateUser(editing.id, {
        email: form.email.trim(),
        name: form.name.trim(),
        companyName: form.companyName.trim(),
        cpf: form.cpf.trim(),
        newPassword: form.password || undefined,
      });
      await authApi.setUserQueues(editing.id, form.queueIds);
      await authApi.setUserPermissions(editing.id, form.permissions);
      await authApi.setUserSessions(editing.id, form.sessionIds);
      toast.success("Usuário atualizado");
      setEditing(null);
      await reload();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const requestRoleChange = (u: AuthUser, nextRole: "admin" | "user") => {
    const isCurrentlyAdmin = u.roles.includes("admin");
    const wantsAdmin = nextRole === "admin";
    if (isCurrentlyAdmin === wantsAdmin) return;
    if (!wantsAdmin && u.id === me?.id) {
      toast.error("Você não pode remover seu próprio papel de administrador.");
      return;
    }
    if (!wantsAdmin && isCurrentlyAdmin && adminCount <= 1) {
      toast.error("É necessário pelo menos um administrador ativo.");
      return;
    }
    setRoleChange({ user: u, nextRole });
  };

  const confirmRoleChange = async () => {
    if (!roleChange) return;
    const { user: u, nextRole } = roleChange;
    setRoleSaving(true);
    try {
      await authApi.setRole(u.id, "admin", nextRole === "admin");
      toast.success(nextRole === "admin" ? `${u.email} agora é administrador.` : `${u.email} agora é atendente.`);
      setRoleChange(null);
      await reload();
      if (auditOpen) void loadAudit();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setRoleSaving(false);
    }
  };

  const remove = async (u: AuthUser) => {
    try {
      await authApi.deleteUser(u.id);
      await reload();
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const dialogOpen = creating || !!editing;
  const closeDialog = () => {
    setCreating(false);
    setEditing(null);
  };

  const Wrapper: ElementType = embedded ? Fragment : AppShell;
  return (
    <Wrapper>
      <div className="max-w-4xl space-y-4">
        <div className="flex items-start justify-between gap-4">
          <p className="text-sm text-muted-foreground">
            {t("pages.users.subtitle")}
          </p>
          {canManage ? (
            <Button onClick={openCreate}>
              <Plus className="mr-1 h-4 w-4" /> {t("pages.users.newUser")}
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">
              {t("pages.users.onlyAdmins")}
            </span>
          )}
        </div>
        {canManage && (
          <div className="rounded-lg border">
            <button
              type="button"
              onClick={() => setAuditOpen((o) => !o)}
              className="flex w-full items-center justify-between px-3 py-2 text-sm hover:bg-muted/40"
            >
              <span className="flex items-center gap-2">
                <History className="h-4 w-4 text-muted-foreground" />
                <span className="font-medium">{t("pages.users.auditTitle")}</span>
              </span>
              <span className="text-xs text-muted-foreground">
                {auditOpen ? t("common.hide") : t("common.show")}
              </span>
            </button>
            {auditOpen && (
              <div className="border-t">
                {auditLoading ? (
                  <div className="flex justify-center py-6">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                ) : audit.length === 0 ? (
                  <p className="px-3 py-6 text-center text-xs text-muted-foreground">
                    Nenhuma mudança registrada ainda.
                  </p>
                ) : (
                  <ul className="divide-y text-xs">
                    {audit.map((e) => (
                      <li key={e.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-medium">{e.actorEmail || e.actorId || "sistema"}</span>
                          <span className="text-muted-foreground">
                            {e.granted ? "concedeu" : "removeu"} o papel
                          </span>
                          <span
                            className={`rounded-full border px-2 py-0.5 font-medium ${
                              e.role === "admin"
                                ? "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-300"
                                : "border-border bg-muted text-muted-foreground"
                            }`}
                          >
                            {e.role}
                          </span>
                          <span className="text-muted-foreground">para</span>
                          <span className="font-medium">{e.targetEmail || e.targetId}</span>
                        </div>
                        <time className="text-muted-foreground">
                          {new Date(e.createdAt * 1000).toLocaleString()}
                        </time>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>
        )}
        {loading ? (
          <div className="flex justify-center py-12">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left">
                <tr>
                  <th className="px-3 py-2 font-medium">Nome / Email</th>
                  <th className="px-3 py-2 font-medium">{t("pages.users.colCompany")}</th>
                  <th className="px-3 py-2 font-medium text-right">{t("pages.users.colActions")}</th>
                </tr>

              </thead>
              <tbody>
                {users.map((u) => {
                  const isMe = u.id === me?.id;
                  const isAdmin = u.roles.includes("admin");
                  return (
                    <tr key={u.id} className="border-t">
                      <td className="px-3 py-2">
                        <div className="flex flex-col leading-tight">
                          <span className="font-medium text-foreground">
                            {u.name?.trim() || u.email}
                            {isMe && <span className="ml-1 text-xs text-muted-foreground">{t("pages.users.you")}</span>}
                          </span>
                          {u.name?.trim() && (
                            <span className="text-xs text-muted-foreground">{u.email}</span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">{u.companyName || "—"}</td>

                      <td className="px-3 py-2">
                        <div className="flex justify-end gap-1">
                          {canManage ? (
                            <>
                              <Button size="sm" variant="outline" onClick={() => void openEdit(u)}>
                                <Pencil className="h-3.5 w-3.5" /> {t("pages.users.edit")}
                              </Button>
                              {!isMe && (
                                <Button size="sm" variant="ghost" onClick={() => setToDelete(u)} title={t("pages.users.deleteTip")}>
                                  <Trash2 className="h-3.5 w-3.5 text-destructive" />
                                </Button>
                              )}
                            </>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
                {users.length === 0 && (
                  <tr>
                    <td colSpan={3} className="px-3 py-8 text-center text-sm text-muted-foreground">
                      Nenhum usuário ainda.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <Dialog open={dialogOpen} onOpenChange={(o) => !o && closeDialog()}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? t("pages.users.dialogEdit") : t("pages.users.dialogNew")}</DialogTitle>
            <DialogDescription>
              {editing
                ? "Atualize dados de acesso e filas vinculadas. Deixe a senha em branco para mantê-la."
                : "Crie um novo usuário e selecione as filas que ele atenderá."}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="col-span-2">
                <Label htmlFor="u-email">Email</Label>
                <Input
                  id="u-email"
                  type="email"
                  value={form.email}
                  onChange={(e) => setForm({ ...form, email: e.target.value })}
                />
              </div>
              <div className="col-span-2">
                <Label htmlFor="u-name">Nome do usuário</Label>
                <Input
                  id="u-name"
                  placeholder="Ex.: João Silva — usado na assinatura do chat"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div className="col-span-2">
                <Label htmlFor="u-pwd">Senha {editing && <span className="text-muted-foreground">(opcional)</span>}</Label>
                <Input
                  id="u-pwd"
                  type="password"
                  placeholder={editing ? "Deixe em branco para manter" : "Mínimo 8 caracteres"}
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="u-company">Empresa</Label>
                <Input
                  id="u-company"
                  value={form.companyName}
                  onChange={(e) => setForm({ ...form, companyName: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="u-cpf">CPF</Label>
                <Input
                  id="u-cpf"
                  value={form.cpf}
                  onChange={(e) => setForm({ ...form, cpf: e.target.value })}
                />
              </div>
            </div>
            <div>
              <Label>Filas vinculadas</Label>
              {queues.length === 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  Nenhuma fila cadastrada. Crie filas em <span className="font-medium">Filas</span>.
                </p>
              ) : (
                <div className="mt-1 flex flex-wrap gap-2">
                  {queues.map((q) => {
                    const on = form.queueIds.includes(q.id);
                    return (
                      <button
                        key={q.id}
                        type="button"
                        onClick={() => toggleQueueInForm(q.id)}
                        className={`rounded-full border px-3 py-1 text-xs transition ${
                          on
                            ? "border-transparent text-white"
                            : "border-border bg-background text-foreground hover:bg-muted"
                        }`}
                        style={on ? { backgroundColor: q.color } : undefined}
                      >
                        {queueById.get(q.id)?.name ?? q.name}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
            <div>
              <Label>Conexões vinculadas</Label>
              {sessions.length === 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  Nenhuma conexão cadastrada. Crie em <span className="font-medium">Conexões</span>.
                </p>
              ) : (
                <div className="mt-1 flex flex-wrap gap-2">
                  {sessions.map((s) => {
                    const on = form.sessionIds.includes(s.id);
                    return (
                      <button
                        key={s.id}
                        type="button"
                        onClick={() => toggleSessionInForm(s.id)}
                        className={`rounded-full border px-3 py-1 text-xs transition ${
                          on
                            ? "border-transparent bg-primary text-primary-foreground"
                            : "border-border bg-background text-foreground hover:bg-muted"
                        }`}
                      >
                        {s.name || s.id}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
            <div>
              <div className="flex items-center justify-between">
                <Label>Perfil de acesso</Label>
                {form.role !== "admin" && (
                  <div className="flex gap-2 text-[11px]">
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground"
                      onClick={() => setAllPermissions(true)}
                    >
                      Marcar todos
                    </button>
                    <span className="text-muted-foreground">·</span>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground"
                      onClick={() => setAllPermissions(false)}
                    >
                      Limpar
                    </button>
                  </div>
                )}
              </div>
              {form.role === "admin" ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  Administradores têm acesso total — todas as permissões são concedidas automaticamente.
                </p>
              ) : (
                <div className="mt-1 grid grid-cols-1 gap-1.5 rounded-md border p-2 sm:grid-cols-2">
                  {PERMISSIONS.map((p) => {
                    const checked = form.permissions.includes(p.key);
                    return (
                      <label
                        key={p.key}
                        className="flex cursor-pointer items-start gap-2 rounded p-1.5 text-xs hover:bg-muted/60"
                      >
                        <input
                          type="checkbox"
                          className="mt-0.5 h-3.5 w-3.5 accent-primary"
                          checked={checked}
                          onChange={() => togglePermissionInForm(p.key)}
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block font-medium leading-tight">{p.label}</span>
                          <span className="block text-[10.5px] text-muted-foreground">
                            {p.description}
                          </span>
                        </span>
                      </label>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={closeDialog} disabled={saving}>
              Cancelar
            </Button>
            <Button
              onClick={() => (editing ? void submitEdit() : void submitCreate())}
              disabled={saving || !form.email || (!editing && !form.password)}
            >
              {saving && <Loader2 className="mr-1 h-4 w-4 animate-spin" />}
              {editing ? "Salvar" : "Criar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={t("pages.users.deleteTitle")}
        description={toDelete ? `${toDelete.email} perderá acesso. Sessões e fluxos ficarão órfãos.` : undefined}
        confirmLabel={t("common.delete")}
        destructive
        onConfirm={() => {
          if (toDelete) void remove(toDelete);
        }}
      />

      <ConfirmDialog
        open={!!roleChange}
        onOpenChange={(o) => !o && !roleSaving && setRoleChange(null)}
        title={roleChange?.nextRole === "admin" ? "Promover a administrador?" : "Rebaixar para atendente?"}
        description={
          roleChange
            ? roleChange.nextRole === "admin"
              ? `${roleChange.user.email} terá acesso total: gerenciar usuários, filas, empresas e conexões.`
              : `${roleChange.user.email} perderá acesso às telas de administração e verá apenas as filas vinculadas.`
            : undefined
        }
        confirmLabel={roleChange?.nextRole === "admin" ? "Promover" : "Rebaixar"}
        destructive={roleChange?.nextRole === "user"}
        onConfirm={() => void confirmRoleChange()}
      />
    </Wrapper>
  );
};

const RoleBadge = ({ role }: { role: "admin" | "user" }) => {
  const isAdmin = role === "admin";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium ${
        isAdmin
          ? "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-300"
          : "border-border bg-muted text-muted-foreground"
      }`}
    >
      {isAdmin ? <Shield className="h-3 w-3" /> : <UserIcon className="h-3 w-3" />}
      {isAdmin ? "Admin" : "Atendente"}
    </span>
  );
};

const RoleSelector = ({
  value,
  disabled,
  onChange,
}: {
  value: "admin" | "user";
  disabled?: boolean;
  onChange: (r: "admin" | "user") => void;
}) => (
  <div className="inline-flex overflow-hidden rounded-md border">
    {(["user", "admin"] as const).map((r) => {
      const active = value === r;
      return (
        <button
          key={r}
          type="button"
          disabled={disabled}
          onClick={() => onChange(r)}
          className={`flex items-center gap-1 px-2 py-1 text-[11px] transition ${
            active
              ? r === "admin"
                ? "bg-amber-500/15 text-amber-700 dark:text-amber-300"
                : "bg-accent text-accent-foreground"
              : "text-muted-foreground hover:bg-muted"
          } ${disabled ? "cursor-not-allowed opacity-60" : ""}`}
        >
          {r === "admin" ? <Shield className="h-3 w-3" /> : <UserIcon className="h-3 w-3" />}
          {r === "admin" ? "Admin" : "Atendente"}
        </button>
      );
    })}
  </div>
);