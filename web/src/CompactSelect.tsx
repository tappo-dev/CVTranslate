import type { ReactNode } from "react";
import * as Select from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";

interface CompactSelectOption {
  value: string;
  label: string;
  description?: string;
}

export function CompactSelect({
  label,
  icon,
  value,
  options,
  placeholder,
  disabled = false,
  onValueChange,
}: {
  label: string;
  icon: ReactNode;
  value: string;
  options: CompactSelectOption[];
  placeholder: string;
  disabled?: boolean;
  onValueChange: (value: string) => void;
}) {
  const selectedOption = options.find((option) => option.value === value);
  const selectionSummary = selectedOption
    ? [selectedOption.label, selectedOption.description]
        .filter(Boolean)
        .join(" \u00b7 ")
    : placeholder;

  return (
    <Select.Root
      value={selectedOption?.value ?? ""}
      onValueChange={onValueChange}
      disabled={disabled || options.length === 0}
    >
      <Select.Trigger
        className="compact-select-trigger"
        aria-label={label}
        title={`${label}: ${selectionSummary}`}
        data-translation-control
      >
        {icon}
        <span className="compact-select-label">{label}</span>
        <span className="compact-select-value">
          <Select.Value placeholder={placeholder}>
            {selectedOption ? selectionSummary : undefined}
          </Select.Value>
        </span>
        <Select.Icon className="compact-select-chevron">
          <ChevronDown size={14} aria-hidden="true" />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal>
        <Select.Content
          className="compact-select-content"
          position="popper"
          side="top"
          align="start"
          sideOffset={8}
          collisionPadding={16}
          aria-label={label}
          data-translation-control
        >
          <Select.ScrollUpButton className="compact-select-scroll">
            <ChevronUp size={16} aria-hidden="true" />
          </Select.ScrollUpButton>
          <Select.Viewport className="compact-select-viewport">
            <Select.Group>
              <Select.Label className="compact-select-heading">
                {label}
              </Select.Label>
              {options.map((option) => (
                <Select.Item
                  key={option.value}
                  value={option.value}
                  textValue={option.label}
                  className="compact-select-option"
                >
                  <Select.ItemText asChild>
                    <span className="compact-select-option-text">
                      <span className="compact-select-option-label">
                        {option.label}
                      </span>
                      {option.description && (
                        <span className="compact-select-description">
                          {option.description}
                        </span>
                      )}
                    </span>
                  </Select.ItemText>
                  <span className="compact-select-check" aria-hidden="true">
                    <Select.ItemIndicator>
                      <Check size={16} />
                    </Select.ItemIndicator>
                  </span>
                </Select.Item>
              ))}
            </Select.Group>
          </Select.Viewport>
          <Select.ScrollDownButton className="compact-select-scroll">
            <ChevronDown size={16} aria-hidden="true" />
          </Select.ScrollDownButton>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  );
}
