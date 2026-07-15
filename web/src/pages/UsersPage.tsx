import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ShieldCheck, Trash2, UserRoundCog, UserRoundPlus } from "lucide-react";
import { toast } from "sonner";
import { ErrorMessage, PageHeader } from "../components/app/chrome";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "../components/ui/alert-dialog";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { Switch } from "../components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { useAuth } from "../hooks/use-auth";
import { createUser, deleteUser, listUsers, updateUser, type AuthRole, type AuthUser } from "../lib/api";
import { formatTime } from "../lib/format";

export function UsersPage() {
  const { user: currentUser } = useAuth();
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: listUsers });
  const [createOpen, setCreateOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<AuthUser | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AuthUser | null>(null);

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["users"] });

  const remove = useMutation({
    mutationFn: (id: string) => deleteUser(id),
    onSuccess: () => {
      invalidate();
      setDeleteTarget(null);
      toast.success("用户已删除");
    },
    onError: (error: Error) => toast.error("删除用户失败", { description: error.message }),
  });

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="用户" description="管理可以访问 FireScribe 的账户：管理员可修改全部设置，普通用户仅可使用功能。">
        <Button onClick={() => setCreateOpen(true)}>
          <UserRoundPlus />
          新建用户
        </Button>
      </PageHeader>

      <ErrorMessage message={users.error?.message} />

      <Card>
        <CardHeader>
          <CardTitle>账户列表</CardTitle>
          <CardDescription>禁用账户会立即失效其登录状态；系统必须保留至少一名启用的管理员。</CardDescription>
        </CardHeader>
        <CardContent>
          {users.isPending ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-2/3" />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户名</TableHead>
                  <TableHead>显示名称</TableHead>
                  <TableHead>角色</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近登录</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(users.data ?? []).map((user) => {
                  const isSelf = currentUser?.id === user.id;
                  return (
                    <TableRow key={user.id}>
                      <TableCell className="font-medium">
                        {user.username}
                        {isSelf ? (
                          <Badge variant="outline" className="ml-2">
                            当前账户
                          </Badge>
                        ) : null}
                      </TableCell>
                      <TableCell>{user.display_name || "-"}</TableCell>
                      <TableCell>
                        {user.role === "admin" ? (
                          <Badge>
                            <ShieldCheck />
                            管理员
                          </Badge>
                        ) : (
                          <Badge variant="secondary">普通用户</Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {user.disabled ? <Badge variant="destructive">已禁用</Badge> : <Badge variant="outline">启用</Badge>}
                      </TableCell>
                      <TableCell className="text-muted-foreground">{formatTime(user.last_login_at) || "从未登录"}</TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => setEditTarget(user)}>
                            <UserRoundCog />
                            编辑
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-destructive hover:text-destructive"
                            disabled={isSelf}
                            title={isSelf ? "不能删除自己的账户" : undefined}
                            onClick={() => setDeleteTarget(user)}
                          >
                            <Trash2 />
                            删除
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={invalidate} />
      {editTarget ? (
        <EditUserDialog
          user={editTarget}
          isSelf={currentUser?.id === editTarget.id}
          onOpenChange={(open) => {
            if (!open) setEditTarget(null);
          }}
          onSaved={invalidate}
        />
      ) : null}

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(open) => (!open ? setDeleteTarget(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除用户 {deleteTarget?.username}？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该账户将立即退出登录且无法恢复。文档、校对等业务数据不会受影响。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={remove.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (deleteTarget) remove.mutate(deleteTarget.id);
              }}
            >
              {remove.isPending ? <Spinner /> : <Trash2 />}
              {remove.isPending ? "删除中" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CreateUserDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}) {
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<AuthRole>("user");

  const create = useMutation({
    mutationFn: () => createUser({ username, password, display_name: displayName, role }),
    onSuccess: (user) => {
      onCreated();
      onOpenChange(false);
      setUsername("");
      setDisplayName("");
      setPassword("");
      setRole("user");
      toast.success(`用户 ${user.username} 已创建`);
    },
    onError: (error: Error) => toast.error("创建用户失败", { description: error.message }),
  });

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!create.isPending) create.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建用户</DialogTitle>
          <DialogDescription>创建后请把用户名和初始密码告知对方，对方可在登录后自行修改密码。</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="create-username">用户名</FieldLabel>
              <Input
                id="create-username"
                required
                autoComplete="off"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
              />
              <FieldDescription>3-32 个字符，可用字母、数字和 . _ -</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="create-display-name">显示名称（可选）</FieldLabel>
              <Input
                id="create-display-name"
                autoComplete="off"
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="create-password">初始密码</FieldLabel>
              <Input
                id="create-password"
                type="password"
                required
                autoComplete="new-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
              <FieldDescription>至少 8 个字符。</FieldDescription>
            </Field>
            <Field>
              <FieldLabel>角色</FieldLabel>
              <Select value={role} onValueChange={(value) => setRole(value as AuthRole)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">普通用户（仅使用功能）</SelectItem>
                  <SelectItem value="admin">管理员（可修改全部设置）</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-6">
            <Button type="button" variant="ghost" disabled={create.isPending} onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? <Spinner /> : <UserRoundPlus />}
              {create.isPending ? "创建中" : "创建用户"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function EditUserDialog({
  user,
  isSelf,
  onOpenChange,
  onSaved,
}: {
  user: AuthUser;
  isSelf: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const [displayName, setDisplayName] = useState(user.display_name);
  const [role, setRole] = useState<AuthRole>(user.role);
  const [enabled, setEnabled] = useState(!user.disabled);
  const [newPassword, setNewPassword] = useState("");

  const save = useMutation({
    mutationFn: () =>
      updateUser(user.id, {
        display_name: displayName,
        role,
        disabled: !enabled,
        ...(newPassword ? { password: newPassword } : {}),
      }),
    onSuccess: () => {
      onSaved();
      onOpenChange(false);
      toast.success(`用户 ${user.username} 已更新`);
    },
    onError: (error: Error) => toast.error("更新用户失败", { description: error.message }),
  });

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!save.isPending) save.mutate();
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>编辑用户 {user.username}</DialogTitle>
          <DialogDescription>修改角色或禁用状态会立即生效；重置密码会使该用户的所有登录状态失效。</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="edit-display-name">显示名称</FieldLabel>
              <Input id="edit-display-name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
            </Field>
            <Field>
              <FieldLabel>角色</FieldLabel>
              <Select value={role} onValueChange={(value) => setRole(value as AuthRole)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">普通用户（仅使用功能）</SelectItem>
                  <SelectItem value="admin">管理员（可修改全部设置）</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <div className="flex items-center justify-between">
                <FieldLabel htmlFor="edit-enabled">启用账户</FieldLabel>
                <Switch id="edit-enabled" checked={enabled} disabled={isSelf} onCheckedChange={setEnabled} />
              </div>
              <FieldDescription>{isSelf ? "不能禁用自己的账户。" : "禁用后该用户会立即退出登录且无法再登录。"}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="edit-password">重置密码（可选）</FieldLabel>
              <Input
                id="edit-password"
                type="password"
                autoComplete="new-password"
                placeholder="留空表示不修改"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
              />
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-6">
            <Button type="button" variant="ghost" disabled={save.isPending} onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? <Spinner /> : <KeyRound />}
              {save.isPending ? "保存中" : "保存修改"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
