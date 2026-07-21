# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.45"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.45/gestaltd-macos-arm64.tar.gz"
      sha256 "94478600f0fa8a05b139e283b23253e29dbd01913515953826b91098c905160c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.45/gestaltd-macos-x86_64.tar.gz"
      sha256 "7293c9a0fec9ce0962c78fa58e0b81b5375ffcaa1e7ab10c31e58d4d1aef385c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.45/gestaltd-linux-arm64.tar.gz"
      sha256 "f154a32ff0da6cc6258cc11d11a1d879c4ee1e712c53a74fa78062d8f8af064e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.45/gestaltd-linux-x86_64.tar.gz"
      sha256 "8725e7af406158a5f3c58601b1ee0d003aa564679ef781bf41faff86512d82fd"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
