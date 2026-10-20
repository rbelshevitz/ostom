using System.Diagnostics;
using System.Globalization;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Ostom.Gui;

internal static class Program
{
    [STAThread]
    private static void Main()
    {
        ApplicationConfiguration.Initialize();
        Application.Run(new MainForm());
    }
}

internal sealed class MainForm : Form
{
    private const long AutoAnalyzeLimit = 1L * 1024 * 1024 * 1024;
    private readonly bool en = !CultureInfo.CurrentUICulture.TwoLetterISOLanguageName.Equals("ru", StringComparison.OrdinalIgnoreCase);
    private readonly TextBox source = new();
    private readonly TextBox output = new();
    private readonly CheckBox eml = new();
    private readonly Button analyze = new();
    private readonly Button run = new();
    private readonly Button cancel = new();
    private readonly ProgressBar progress = new();
    private readonly RichTextBox result = new();
    private Process? child;
    private CancellationTokenSource? cancellation;

    private string T(string ru, string english) => en ? english : ru;

    public MainForm()
    {
        Text = T("OSTOM — восстановление PST/OST", "OSTOM — PST/OST recovery");
        StartPosition = FormStartPosition.CenterScreen;
        ClientSize = new Size(780, 470);
        MinimumSize = new Size(680, 400);
        Font = new Font("Segoe UI", 9F);
        BuildLayout();
    }

    private void BuildLayout()
    {
        var layout = new TableLayoutPanel { Dock = DockStyle.Fill, Padding = new Padding(14), ColumnCount = 3, RowCount = 8 };
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 24));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 34));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 24));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 34));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 38));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 22));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.Absolute, 8));
        Controls.Add(layout);

        layout.Controls.Add(new Label { Text = T("Источник PST/OST", "PST/OST source file"), Dock = DockStyle.Fill, TextAlign = ContentAlignment.BottomLeft }, 0, 0);
        source.Dock = DockStyle.Fill;
        layout.Controls.Add(source, 0, 1);
        var open = MakeActionButton(T("Открыть…", "Open…"));
        open.Click += async (_, _) => await PickSourceAsync();
        layout.Controls.Add(open, 1, 1);
        layout.SetColumnSpan(source, 1);

        layout.Controls.Add(new Label { Text = T("Папка экспорта mbox", "mbox export folder"), Dock = DockStyle.Fill, TextAlign = ContentAlignment.BottomLeft }, 0, 2);
        output.Dock = DockStyle.Fill;
        layout.Controls.Add(output, 0, 3);
        var folder = MakeActionButton(T("Папка…", "Folder…"));
        folder.Click += (_, _) => PickFolder();
        layout.Controls.Add(folder, 1, 3);

        var actions = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 4, RowCount = 1, Padding = new Padding(0) };
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        actions.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 140));
        eml.Text = T("Дополнительно сохранить EML", "Save individual EML files too");
        eml.AutoSize = false;
        eml.Dock = DockStyle.Fill;
        actions.Controls.Add(eml);
        ConfigureActionButton(analyze, T("Анализ", "Analyze"));
        analyze.Click += async (_, _) => { if (!string.IsNullOrWhiteSpace(source.Text)) await AnalyzeAsync(source.Text); };
        actions.Controls.Add(analyze);
        ConfigureActionButton(run, T("Экспорт", "Export"));
        run.Click += async (_, _) => await RunAsync();
        actions.Controls.Add(run);
        ConfigureActionButton(cancel, T("Отмена", "Cancel"));
        cancel.Enabled = false;
        cancel.Click += (_, _) => cancellation?.Cancel();
        actions.Controls.Add(cancel);
        layout.Controls.Add(actions, 0, 4);
        layout.SetColumnSpan(actions, 3);

        progress.Dock = DockStyle.Fill;
        progress.Margin = new Padding(0, 6, 0, 0);
        progress.MinimumSize = new Size(0, 18);
        progress.Style = ProgressBarStyle.Marquee;
        progress.MarqueeAnimationSpeed = 28;
        progress.Visible = false;
        layout.Controls.Add(progress, 0, 5);
        layout.SetColumnSpan(progress, 3);

        result.Dock = DockStyle.Fill;
        result.ReadOnly = true;
        result.BackColor = SystemColors.Window;
        result.ScrollBars = RichTextBoxScrollBars.Vertical;
        layout.Controls.Add(result, 0, 6);
        layout.SetColumnSpan(result, 3);
        FormClosing += (_, _) => { if (child is { HasExited: false }) child.Kill(entireProcessTree: true); };
    }

    private static Button MakeActionButton(string text)
    {
        var button = new Button();
        ConfigureActionButton(button, text);
        return button;
    }

    private static void ConfigureActionButton(Button button, string text)
    {
        button.Text = text;
        button.AutoSize = false;
        button.Dock = DockStyle.Fill;
        button.Margin = new Padding(3, 1, 3, 1);
        button.MinimumSize = new Size(0, 32);
        button.Height = 32;
        button.Padding = new Padding(8, 0, 8, 0);
    }

    private async Task PickSourceAsync()
    {
        using var dialog = new OpenFileDialog { Filter = T("Файлы Outlook (*.pst;*.ost)|*.pst;*.ost", "Outlook files (*.pst;*.ost)|*.pst;*.ost"), CheckFileExists = true, Multiselect = false };
        if (dialog.ShowDialog(this) != DialogResult.OK) return;
        var ext = Path.GetExtension(dialog.FileName).ToLowerInvariant();
        if (ext is not ".pst" and not ".ost") return;
        source.Text = dialog.FileName;
        output.Text = dialog.FileName + ".ostom-export";
        var size = new FileInfo(dialog.FileName).Length;
        if (size <= AutoAnalyzeLimit)
            await AnalyzeAsync(dialog.FileName);
        else
            result.Text = T($"Файл большой ({FormatBytes(size)}). Нажмите «Анализ», когда будете готовы проверить содержимое.", $"Large file ({FormatBytes(size)}). Click Analyze when you are ready to inspect its contents.");
    }

    private void PickFolder()
    {
        using var dialog = new FolderBrowserDialog { Description = T("Выберите папку для экспорта mbox", "Select the mbox export folder"), UseDescriptionForTitle = true, ShowNewFolderButton = true };
        if (dialog.ShowDialog(this) == DialogResult.OK)
        {
            output.Text = dialog.SelectedPath;
            ShowDiskSpace(dialog.SelectedPath);
        }
    }

    private async Task RunAsync()
    {
        if (string.IsNullOrWhiteSpace(source.Text) || string.IsNullOrWhiteSpace(output.Text))
        {
            result.Text = T("Укажите исходный файл и папку экспорта.", "Select a source file and an export folder.");
            return;
        }
        var cli = Path.Combine(AppContext.BaseDirectory, "ostom.exe");
        if (!File.Exists(cli))
        {
            result.Text = T("Рядом с GUI не найден ostom.exe.", "ostom.exe was not found alongside the GUI.");
            return;
        }
        SetBusy(true);
        cancellation = new CancellationTokenSource();
        try
        {
            var args = new List<string> { "--json", "--progress-json", "--output-dir", output.Text, "--export-dir", output.Text };
            if (eml.Checked) args.Add("--eml");
            args.Add(source.Text);
            ShowFriendly(T("Экспорт выполняется…", "Export is running…"));
            var report = await RunCliAsync(cli, args, cancellation.Token);
            ShowReport(report);
        }
        catch (OperationCanceledException)
        {
            if (child is { HasExited: false }) child.Kill(entireProcessTree: true);
            result.Text = T("Операция отменена.", "Operation cancelled.");
        }
        finally
        {
            SetBusy(false);
            cancellation?.Dispose();
            cancellation = null;
            child?.Dispose();
            child = null;
        }
    }

    private async Task AnalyzeAsync(string file)
    {
        if (child is { HasExited: false }) return;
        var cli = Path.Combine(AppContext.BaseDirectory, "ostom.exe");
        if (!File.Exists(cli)) { ShowFriendly(T("Рядом с GUI не найден ostom.exe.", "ostom.exe was not found alongside the GUI.")); return; }
        SetBusy(true);
        cancellation = new CancellationTokenSource();
        try
        {
            ShowFriendly(T("Анализ файла…", "Analyzing file…"));
            var args = new List<string> { "--json", "--analyze", "--progress-json" };
            if (!string.IsNullOrWhiteSpace(output.Text)) args.AddRange(new[] { "--output-dir", output.Text });
            args.Add(file);
            var report = await RunCliAsync(cli, args, cancellation.Token);
            ShowReport(report);
        }
        catch (OperationCanceledException) { ShowFriendly(T("Анализ отменён.", "Analysis cancelled.")); }
        catch (Exception ex) { ShowFriendly(T("Не удалось выполнить анализ: " + ex.Message, "Analysis failed: " + ex.Message)); }
        finally
        {
            SetBusy(false);
            cancellation?.Dispose();
            cancellation = null;
            child?.Dispose();
            child = null;
        }
    }

    private async Task<string> RunCliAsync(string cli, IEnumerable<string> args, CancellationToken token)
    {
        var psi = new ProcessStartInfo(cli) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardOutput = true, RedirectStandardError = true, WorkingDirectory = AppContext.BaseDirectory };
        foreach (var arg in args) psi.ArgumentList.Add(arg);
        child = new Process { StartInfo = psi };
        child.Start();
        var stdout = child.StandardOutput.ReadToEndAsync(token);
        var progressOutput = DrainProgressAsync(child.StandardError, token);
        var wait = child.WaitForExitAsync(token);
        await Task.WhenAll(stdout, progressOutput, wait);
        return await stdout;
    }

    private async Task DrainProgressAsync(StreamReader reader, CancellationToken token)
    {
        while (await reader.ReadLineAsync(token) is { } line) HandleProgress(line);
    }

    private void HandleProgress(string line)
    {
        try
        {
            using var doc = JsonDocument.Parse(line);
            var root = doc.RootElement;
            if (!root.TryGetProperty("event", out var kind) || kind.GetString() != "progress") return;
            var label = root.GetProperty("label").GetString() ?? "";
            var done = root.GetProperty("done").GetInt32();
            var total = root.GetProperty("total").GetInt32();
            if (!IsDisposed) BeginInvoke(() =>
            {
                if (total > 0)
                {
                    progress.Visible = true;
                    progress.Style = ProgressBarStyle.Continuous;
                    progress.Maximum = Math.Max(1, total);
                    progress.Value = Math.Min(Math.Max(0, done), progress.Maximum);
                }
                else
                {
                    progress.Visible = true;
                    progress.Style = ProgressBarStyle.Marquee;
                }
                result.Text = total > 0
                    ? $"{T("Обработано", "Processed")}: {done} {T("из", "of")} {total}"
                    : $"{T("Обработано", "Processed")}: {done}";
            });
        }
        catch (JsonException) { }
    }

    private string FriendlyProgress(string value) => value switch
    {
        "scanning recoverable messages" => T("Обнаружено сообщений", "Messages found"),
        "writing recovered messages" => T("Восстановлено сообщений", "Messages recovered"),
        "standard mailbox export" => T("Обработано сообщений", "Messages processed"),
        "analyzing recoverable messages" => T("Проверено сообщений", "Messages checked"),
        _ => value
    };

    private void ShowReport(string json)
    {
        try
        {
            var report = JsonSerializer.Deserialize<GuiReport>(json);
            if (report is null) throw new InvalidDataException();
            var indexedTimeout = report.Warnings.Any(w => w.StartsWith("indexed export: indexed worker exceeded", StringComparison.OrdinalIgnoreCase));
            var fallbackUsed = report.NativeRecovery?.Export is { Written: > 0 };
            var text = new StringBuilder();
            text.AppendLine($"{T("Состояние", "Status")}: {FriendlyStatus(report.Status, report.CheckOnly, indexedTimeout && !fallbackUsed, report.Analysis is not null)}");
            text.AppendLine($"{T("Структура", "Structure")}: {FriendlyFinding(report.Findings.Structure, report.CheckOnly)}");
            var messagesFinding = FriendlyFinding(report.Findings.Messages, report.CheckOnly);
            var attachmentsFinding = FriendlyFinding(report.Findings.Attachments, report.CheckOnly);
            if (indexedTimeout && report.NativeRecovery?.Export is not null)
            {
                messagesFinding = fallbackUsed
                    ? T($"восстановлено {report.NativeRecovery.Export.Written}", $"{report.NativeRecovery.Export.Written} recovered")
                    : T("не удалось завершить чтение файла", "file reading could not be completed");
                attachmentsFinding = T($"восстановлено {report.NativeRecovery.Export.Attachments}", $"{report.NativeRecovery.Export.Attachments} recovered");
            }
            text.AppendLine($"{T("Сообщения", "Messages")}: {messagesFinding}");
            text.AppendLine($"{T("Вложения", "Attachments")}: {attachmentsFinding}");
            if (report.Analysis is not null)
                text.AppendLine($"{T("Рекомендация", "Recommendation")}: {FriendlyOutlook(report.Assessment.RecoveryOutlook, report.Findings.Structure, true, fallbackUsed)}");
            if (report.Analysis is not null)
            {
                text.AppendLine($"{T("Найдено сообщений-кандидатов", "Messages found for recovery")}: {report.Analysis.Candidates}");
                text.AppendLine($"{T("Найдено вложений", "Attachments found for recovery")}: {report.Analysis.Attachments}");
                if (report.Analysis.IncompleteMessages > 0) text.AppendLine($"{T("Неполных сообщений", "Incomplete messages")}: {report.Analysis.IncompleteMessages}");
                if (report.Analysis.IncompleteAttachments > 0) text.AppendLine($"{T("Неполных вложений", "Incomplete attachments")}: {report.Analysis.IncompleteAttachments}");
                if (report.Analysis.Rejected > 0) text.AppendLine($"{T("Пропущено кандидатов", "Rejected candidates")}: {report.Analysis.Rejected}");
                if (report.Analysis.LimitReached) text.AppendLine(T("Анализ достиг безопасного лимита; экспорт можно продолжить отдельно.", "The safe analysis limit was reached; export can continue separately."));
            }
            else if (report.CheckOnly) text.AppendLine(T("Это быстрая проверка структуры; содержимое писем и вложений будет проверено во время экспорта.", "This is a fast structural check; message and attachment content will be checked during export."));
            if (report.Storage is not null && report.Analysis is not null) text.AppendLine($"{T("Свободно на диске", "Free disk space")}: {FormatBytes(report.Storage.AvailableBytes)}; {T("оценка экспорта", "export estimate")}: {FormatBytes(report.Storage.RequiredEstimate)}");
            else if (!fallbackUsed && report.IndexedExport is not null) text.AppendLine($"{T("Экспортировано", "Exported")}: {report.IndexedExport.Messages} {T("сообщений", "messages")}, {report.IndexedExport.Attachments} {T("вложений", "attachments")}");
            if (!fallbackUsed && report.NativeRecovery?.Export is not null) text.AppendLine($"{T("Восстановлено", "Recovered")}: {report.NativeRecovery.Export.Written} {T("сообщений", "messages")}, {report.NativeRecovery.Export.Attachments} {T("вложений", "attachments")}");
            foreach (var warning in report.Warnings.Where(w => !w.Equals("check-only mode: export was not requested", StringComparison.OrdinalIgnoreCase) && !w.StartsWith("indexed export: indexed worker exceeded", StringComparison.OrdinalIgnoreCase))) text.AppendLine($"{T("Предупреждение", "Warning")}: {warning}");
            foreach (var error in report.Errors) text.AppendLine($"{T("Ошибка", "Error")}: {error}");
            result.Text = text.ToString();
        }
        catch { result.Text = T("Не удалось прочитать итоговый отчёт CLI.", "Could not read the CLI final report."); }
    }

    private void ShowFriendly(string text) => result.Text = text;

    private string FriendlyStatus(string value, bool checkOnly, bool indexedTimeout, bool contentAnalysis = false) => indexedTimeout ? T("Завершено частично", "Completed with limitations") : value switch
    {
        "completed" when checkOnly || contentAnalysis => T("Анализ завершён", "Analysis complete"),
        "completed" => T("Готово", "Complete"),
        "completed with errors" => T("Завершено с предупреждениями", "Completed with warnings"),
        "failed" => T("Не удалось выполнить", "Failed"),
        _ => value
    };

    private void ShowDiskSpace(string path)
    {
        try
        {
            var root = Path.GetPathRoot(Path.GetFullPath(path));
            if (!string.IsNullOrEmpty(root))
            {
                var drive = new DriveInfo(root);
                ShowFriendly($"{T("Свободно на диске", "Free disk space")}: {FormatBytes((ulong)drive.AvailableFreeSpace)}\r\n" +
                    T("Место проверено; оценка содержимого появится после анализа.", "Space checked; the content estimate will appear after analysis."));
            }
        }
        catch { }
    }

    private string FriendlyFinding(string value, bool checkOnly) => value switch
    {
        "not_checked" when checkOnly => T("будут проверены при экспорте", "will be checked during export"),
        "not_checked" => T("ещё не проверялись", "not checked yet"),
        "healthy" => T("прочиталась нормально", "read successfully"),
        "complete" => T("найдены и прочитаны", "available and readable"),
        "degraded" => T("есть повреждения", "damaged or incomplete"),
        "partial" => T("частично восстановлены", "partially recovered"),
        "timeout" => T("обычный проход остановлен защитным лимитом", "regular pass stopped by safety limit"),
        "failed" => T("не удалось прочитать", "could not be read"),
        _ => value
    };

    private string FriendlyOutlook(string value, string structure, bool contentAnalysis, bool fallbackUsed) => fallbackUsed
        ? T("восстановление выполнено; результат сохранён", "recovery completed; result saved")
        : structure == "healthy"
        ? T("восстановление не требуется; можно экспортировать", "no recovery is needed; ready to export")
        : value switch
    {
        "good" => T("хорошая", "good"),
        "uncertain" => T("нужна проверка повреждённых участков", "some damaged areas need checking"),
        "limited" => T("ограниченная", "recovery may be limited"),
        _ => value
    };

    private static string FormatBytes(long value) => FormatBytes((ulong)Math.Max(0, value));
    private static string FormatBytes(ulong value)
    {
        var units = new[] { "B", "KiB", "MiB", "GiB", "TiB" };
        double n = value; var i = 0;
        while (n >= 1024 && i < units.Length - 1) { n /= 1024; i++; }
        return $"{n:0.0} {units[i]}";
    }

    private void SetBusy(bool busy)
    {
        run.Enabled = !busy;
        analyze.Enabled = !busy && !string.IsNullOrWhiteSpace(source.Text);
        eml.Enabled = !busy;
        cancel.Enabled = busy;
        progress.Visible = busy;
        UseWaitCursor = busy;
    }
}

internal sealed class GuiReport
{
    [JsonPropertyName("status")] public string Status { get; set; } = "";
    [JsonPropertyName("check_only")] public bool CheckOnly { get; set; }
    [JsonPropertyName("findings")] public GuiFindings Findings { get; set; } = new();
    [JsonPropertyName("assessment")] public GuiAssessment Assessment { get; set; } = new();
    [JsonPropertyName("storage")] public GuiStorage? Storage { get; set; }
    [JsonPropertyName("indexed_export")] public GuiIndexed? IndexedExport { get; set; }
    [JsonPropertyName("native_recovery")] public GuiNative? NativeRecovery { get; set; }
    [JsonPropertyName("analysis")] public GuiAnalysis? Analysis { get; set; }
    [JsonPropertyName("warnings")] public List<string> Warnings { get; set; } = new();
    [JsonPropertyName("errors")] public List<string> Errors { get; set; } = new();
}
internal sealed class GuiFindings { [JsonPropertyName("structure")] public string Structure { get; set; } = ""; [JsonPropertyName("messages")] public string Messages { get; set; } = ""; [JsonPropertyName("attachments")] public string Attachments { get; set; } = ""; }
internal sealed class GuiAssessment { [JsonPropertyName("recovery_outlook")] public string RecoveryOutlook { get; set; } = ""; }
internal sealed class GuiStorage { [JsonPropertyName("available_bytes")] public ulong AvailableBytes { get; set; } [JsonPropertyName("required_estimate_bytes")] public ulong RequiredEstimate { get; set; } }
internal sealed class GuiIndexed { [JsonPropertyName("messages")] public int Messages { get; set; } [JsonPropertyName("attachments")] public int Attachments { get; set; } }
internal sealed class GuiNative { [JsonPropertyName("export")] public GuiNativeExport? Export { get; set; } }
internal sealed class GuiNativeExport { [JsonPropertyName("written")] public int Written { get; set; } [JsonPropertyName("attachments")] public int Attachments { get; set; } }
internal sealed class GuiAnalysis
{
    [JsonPropertyName("candidates")] public int Candidates { get; set; }
    [JsonPropertyName("attachments")] public int Attachments { get; set; }
    [JsonPropertyName("incomplete_messages")] public int IncompleteMessages { get; set; }
    [JsonPropertyName("incomplete_attachments")] public int IncompleteAttachments { get; set; }
    [JsonPropertyName("rejected")] public int Rejected { get; set; }
    [JsonPropertyName("limit_reached")] public bool LimitReached { get; set; }
}
