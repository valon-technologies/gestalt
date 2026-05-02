# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.8/gestaltd-macos-arm64.tar.gz"
      sha256 "d290ea8a12fb6d139ea47885fe53cebb1c6d3037fa576a296fcea6c53fa82cca"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.8/gestaltd-macos-x86_64.tar.gz"
      sha256 "b5efe1bb3b20508bf21b830a47f7dda0c00fc74d41ecde9bfb572a9b8dc128ea"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.8/gestaltd-linux-arm64.tar.gz"
      sha256 "245e3d6024d1ad1bb205671b62581a3df37235d7ab7fab9f99d5eb4c51240cf3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.8/gestaltd-linux-x86_64.tar.gz"
      sha256 "8207b71eb3ea32bb3783c7bc171368910e48d56cca02d23f82cba7b03dac14f9"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
