import { useState, type FormEvent } from "react";
import { BookOpenText, KeyRound, LogIn, UserRoundPlus } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "../components/ui/alert";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "../components/ui/field";
import { Input } from "../components/ui/input";
import { Spinner } from "../components/ui/spinner";
import { useAuth } from "../hooks/use-auth";
import { ApiError, login, setupInitialAdmin } from "../lib/api";

function loginErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      if (error.message.includes("disabled")) return "该账户已被禁用，请联系管理员。";
      return "用户名或密码错误。";
    }
    return error.message;
  }
  return "网络错误，请稍后重试。";
}

export function LoginPage({ mode }: { mode: "login" | "setup" }) {
  const { refresh } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const isSetup = mode === "setup";

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    setError("");
    if (isSetup) {
      if (password.length < 8) {
        setError("密码至少需要 8 个字符。");
        return;
      }
      if (password !== confirmPassword) {
        setError("两次输入的密码不一致。");
        return;
      }
    }
    setPending(true);
    try {
      if (isSetup) {
        await setupInitialAdmin({ username, password, display_name: displayName });
      } else {
        await login({ username, password });
      }
      await refresh();
    } catch (submitError) {
      setError(isSetup && submitError instanceof ApiError ? submitError.message : loginErrorMessage(submitError));
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-4">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center justify-center gap-2 text-lg font-semibold">
          <BookOpenText className="size-6" />
          <span>FireScribe</span>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{isSetup ? "初始化管理员账户" : "登录"}</CardTitle>
            <CardDescription>
              {isSetup
                ? "首次使用 FireScribe，请先创建管理员账户。该账户可以管理设置和其他用户。"
                : "请输入账户信息以继续使用文档工作台。"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit}>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="login-username">用户名</FieldLabel>
                  <Input
                    id="login-username"
                    autoComplete="username"
                    autoFocus
                    required
                    value={username}
                    onChange={(event) => setUsername(event.target.value)}
                  />
                  {isSetup ? <FieldDescription>3-32 个字符，可用字母、数字和 . _ -</FieldDescription> : null}
                </Field>
                {isSetup ? (
                  <Field>
                    <FieldLabel htmlFor="login-display-name">显示名称（可选）</FieldLabel>
                    <Input
                      id="login-display-name"
                      autoComplete="name"
                      value={displayName}
                      onChange={(event) => setDisplayName(event.target.value)}
                    />
                  </Field>
                ) : null}
                <Field>
                  <FieldLabel htmlFor="login-password">密码</FieldLabel>
                  <Input
                    id="login-password"
                    type="password"
                    autoComplete={isSetup ? "new-password" : "current-password"}
                    required
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                  />
                  {isSetup ? <FieldDescription>至少 8 个字符。</FieldDescription> : null}
                </Field>
                {isSetup ? (
                  <Field>
                    <FieldLabel htmlFor="login-confirm-password">确认密码</FieldLabel>
                    <Input
                      id="login-confirm-password"
                      type="password"
                      autoComplete="new-password"
                      required
                      value={confirmPassword}
                      onChange={(event) => setConfirmPassword(event.target.value)}
                    />
                  </Field>
                ) : null}

                {error ? (
                  <Alert variant="destructive">
                    <KeyRound />
                    <AlertTitle>{isSetup ? "初始化失败" : "登录失败"}</AlertTitle>
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                ) : null}

                <Button type="submit" className="w-full" disabled={pending}>
                  {pending ? <Spinner /> : isSetup ? <UserRoundPlus /> : <LogIn />}
                  {pending ? (isSetup ? "创建中…" : "登录中…") : isSetup ? "创建管理员并进入" : "登录"}
                </Button>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
