import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "../ui/field";
import { Input } from "../ui/input";
import { Spinner } from "../ui/spinner";
import { changeOwnPassword } from "../../lib/api";

export function ChangePasswordDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  const change = useMutation({
    mutationFn: () => changeOwnPassword({ current_password: currentPassword, new_password: newPassword }),
    onSuccess: () => {
      onOpenChange(false);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      toast.success("密码已修改", { description: "其他设备上的登录状态已全部退出。" });
    },
    onError: (error: Error) =>
      toast.error("修改密码失败", {
        description: error.message.includes("incorrect") ? "当前密码不正确。" : error.message,
      }),
  });

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (change.isPending) return;
    if (newPassword.length < 8) {
      toast.error("新密码至少需要 8 个字符");
      return;
    }
    if (newPassword !== confirmPassword) {
      toast.error("两次输入的新密码不一致");
      return;
    }
    change.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>修改密码</DialogTitle>
          <DialogDescription>修改成功后，除当前浏览器外的其他登录状态会被退出。</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="change-current-password">当前密码</FieldLabel>
              <Input
                id="change-current-password"
                type="password"
                required
                autoComplete="current-password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="change-new-password">新密码</FieldLabel>
              <Input
                id="change-new-password"
                type="password"
                required
                autoComplete="new-password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
              />
              <FieldDescription>至少 8 个字符。</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="change-confirm-password">确认新密码</FieldLabel>
              <Input
                id="change-confirm-password"
                type="password"
                required
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
              />
            </Field>
          </FieldGroup>
          <DialogFooter className="mt-6">
            <Button type="button" variant="ghost" disabled={change.isPending} onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={change.isPending}>
              {change.isPending ? <Spinner /> : <KeyRound />}
              {change.isPending ? "提交中" : "确认修改"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
